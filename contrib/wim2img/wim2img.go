// +build windows

// This program takes as input a path to a WIM file containing a Windows container base image
// and produces as output a tar file that can be passed to docker load.
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio/wim"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/digest"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types/container"
)

type manifest struct {
	Config   string
	RepoTags []string
	Layers   []string
}

type rootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids,omitempty"`
}

type image struct {
	Architecture string            `json:"architecture,omitempty"`
	Config       *container.Config `json:"config,omitempty"`
	Created      time.Time         `json:"created"`
	OS           string            `json:"os,omitempty"`
	OSVersion    string            `json:"os.version,omitempty"`
	OSFeatures   []string          `json:"os.features,omitempty"`
	RootFS       *rootFS           `json:"rootfs,omitempty"`
}

type config struct {
	Repo      string
	Cmd       string
	TagLatest bool
}

func writeImage(out io.Writer, wr *wim.Reader, c *config) error {
	wimimg := wr.Image[0]
	if wimimg.Windows == nil {
		return errors.New("not a Windows image")
	}

	layerName := "layer.tar"
	manifestName := "manifest.json"
	configName := "config.json"

	repo := c.Repo
	if repo == "" {
		repo = strings.ToLower(wimimg.Name)
	}

	m := []manifest{
		{
			Config: configName,
			Layers: []string{layerName},
		},
	}

	v := &wimimg.Windows.Version
	m[0].RepoTags = append(m[0].RepoTags, fmt.Sprintf("%s:%d.%d.%d.%d", repo, v.Major, v.Minor, v.Build, v.SPBuild))

	if c.TagLatest {
		m[0].RepoTags = append(m[0].RepoTags, repo+":latest")
	}

	mj, err := json.Marshal(m)
	if err != nil {
		return err
	}

	tw := tar.NewWriter(out)
	hdr := tar.Header{
		Name:     manifestName,
		Mode:     0644,
		Typeflag: tar.TypeReg,
		Size:     int64(len(mj)),
	}

	err = tw.WriteHeader(&hdr)
	if err != nil {
		return err
	}

	_, err = tw.Write(mj)
	if err != nil {
		return err
	}

	size, err := wimTarSize(wr)
	if err != nil {
		return err
	}

	hdr = tar.Header{
		Name:     layerName,
		Mode:     0644,
		Typeflag: tar.TypeReg,
		Size:     size,
	}

	err = tw.WriteHeader(&hdr)
	if err != nil {
		return err
	}

	d := digest.Canonical.New()
	mw := io.MultiWriter(tw, d.Hash())
	err = writeWimTar(mw, wr, false)
	if err != nil {
		return err
	}

	img := image{
		OS:        "windows",
		OSVersion: fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build),
		Created:   wimimg.CreationTime.Time(),
		RootFS: &rootFS{
			Type:    "layers",
			DiffIDs: []string{string(d.Digest())},
		},
	}

	if c.Cmd != "" {
		img.Config = &container.Config{
			Cmd: []string{c.Cmd},
		}
	}

	switch wimimg.Windows.Arch {
	case wim.PROCESSOR_ARCHITECTURE_AMD64:
		img.Architecture = "amd64"
	default:
		return fmt.Errorf("unknown architecture value %d", wimimg.Windows.Arch)
	}

	if wimimg.Name != "NanoServer" {
		img.OSFeatures = append(img.OSFeatures, "win32k")
	}

	imgj, err := json.Marshal(img)
	if err != nil {
		return err
	}

	hdr = tar.Header{
		Name:     configName,
		Mode:     0644,
		Typeflag: tar.TypeReg,
		Size:     int64(len(imgj)),
	}

	err = tw.WriteHeader(&hdr)
	if err != nil {
		return err
	}

	_, err = tw.Write(imgj)
	if err != nil {
		return err
	}

	err = tw.Flush()
	if err != nil {
		return err
	}

	return nil
}

func combineErrors(errs ...error) error {
	var err error
	for _, e := range errs {
		if err == nil {
			err = e
		}
	}
	return err
}

func main() {
	c := &config{}

	outname := flag.String("out", "", "Output tar file. If not present, writes to stdout.")
	flag.BoolVar(&c.TagLatest, "latest", false, "Include a :latest tag on the image.")
	flag.StringVar(&c.Repo, "repo", "", "Repo name (defaults to WIM image name).")
	flag.StringVar(&c.Cmd, "cmd", `c:\windows\system32\cmd.exe`, "Default command.")
	layerOnly := flag.Bool("layeronly", false, "Only export the layer tar, not a full image.")
	gz := flag.Bool("z", false, "gzip the resulting tar")
	load := flag.Bool("load", false, "instead of writing the image, load it into the docker engine")
	flag.Parse()
	filename := flag.Arg(0)

	wimfile, err := os.Open(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer wimfile.Close()

	wr, err := wim.NewReader(wimfile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var out io.WriteCloser = os.Stdout
	var pr *io.PipeReader
	if *load {
		pr, out = io.Pipe()
	} else if *outname != "" {
		outf, err := os.Create(*outname)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		out = outf
	}

	var gzout *gzip.Writer
	if *gz {
		gzout = gzip.NewWriter(out)
		out = gzout
	}

	errc := make(chan error)
	go func() {
		var err error
		if *layerOnly {
			err = writeWimTar(out, wr, false)
		} else {
			err = writeImage(out, wr, c)
		}
		if gzout != nil {
			err = combineErrors(err, gzout.Close())
		}

		if out != os.Stdout {
			err = combineErrors(err, out.Close())
		}

		errc <- err
	}()

	if *load {
		cli, err := client.NewEnvClient()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		resp, err := cli.ImageLoad(context.Background(), pr, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		if resp.JSON {
			var m uint32
			isTerminal := syscall.GetConsoleMode(syscall.Handle(os.Stdout.Fd()), &m) == nil
			err = jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, os.Stdout.Fd(), isTerminal, nil)
		} else {
			_, err = io.Copy(os.Stdout, resp.Body)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	err = <-errc
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
