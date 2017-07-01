package lcow

import (
	"fmt"
	"io"

	"bytes"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/chrootarchive"
	"github.com/docker/docker/pkg/rootfs"
	"github.com/jhowardmsft/opengcs/gogcs/client"
)

type lcowfs struct {
	root   string
	config client.Config
}

var _ rootfs.RootFS = &lcowfs{}

var ErrNotSupported = fmt.Errorf("not supported")

// Functions to implement the rootfs interface
func (l *lcowfs) Path() string {
	return l.root
}

func (l *lcowfs) ResolveScopedPath(path string) (string, error) {
	logrus.Debugf("XXX: EVALSYMLINK %s %s ", path, l.root)
	arg1 := l.Join(l.root, path)
	arg2 := l.root

	output := &bytes.Buffer{}
	cmd := fmt.Sprintf("remotefs resolvepath %s %s", arg1, arg2)
	err := l.config.RunProcess(cmd, nil, output)
	if err != nil {
		return "", err
	}

	logrus.Debugf("XXX: GOT RESOLVED PATH: %s\n", output.String())

	return output.String(), nil
}

func (l *lcowfs) Platform() string {
	return "linux"
}

// Other functions that are used by docker like the daemon Archiver/Extractor
func (l *lcowfs) ExtractArchive(src io.Reader, dst string, opts *archive.TarOptions) error {
	return chrootarchive.Untar(src, dst, opts)
}

func (l *lcowfs) ArchivePath(src string, opts *archive.TarOptions) (io.ReadCloser, error) {
	return archive.TarWithOptions(src, opts)
}
