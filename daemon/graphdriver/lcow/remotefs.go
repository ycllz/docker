package lcow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"encoding/binary"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/rootfs"
)

type lcowfs struct {
	root        string
	d           *Driver
	mappedDisks []hcsshim.MappedVirtualDisk
	vmID        string
	currentSVM  serviceVM
}

var _ rootfs.RootFS = &lcowfs{}

// ErrNotSupported is an error for unsupported operations in the remotefs
var ErrNotSupported = fmt.Errorf("not supported")

// Functions to implement the rootfs interface
func (l *lcowfs) Path() string {
	return l.root
}

func (l *lcowfs) ResolveScopedPath(path string) (string, error) {
	logrus.Debugf("REMOTEFS: EVALSYMLINK %s %s ", path, l.root)
	arg1 := l.Join(l.root, path)
	arg2 := l.root

	output := &bytes.Buffer{}
	cmd := fmt.Sprintf("remotefs resolvepath %s %s", arg1, arg2)
	process, err := l.currentSVM.config.RunProcess(cmd, nil, output, nil)
	if err != nil {
		return "", err
	}
	process.Close()

	logrus.Debugf("REMOTEFS: GOT RESOLVED PATH: %s\n", output.String())

	return output.String(), nil
}

func (l *lcowfs) Platform() string {
	return "linux"
}

// Other functions that are used by docker like the daemon Archiver/Extractor
func (l *lcowfs) ExtractArchive(src io.Reader, dst string, opts *archive.TarOptions) error {
	logrus.Debugf("REMOTEFS: extract archive: %s %+v", dst, opts)

	optsBuf, err := json.Marshal(opts)
	if err != nil {
		return fmt.Errorf("failed to marshall tar opts: %s", err)
	}

	// Need to send size first, so that the json package knowns when to stop reading.
	optsSize := uint64(len(optsBuf))
	optsSizeBuf := &bytes.Buffer{}
	if err := binary.Write(optsSizeBuf, binary.BigEndian, optsSize); err != nil {
		return fmt.Errorf("failed to marshall tar opts size: %s", err)
	}

	input := io.MultiReader(optsSizeBuf, bytes.NewBuffer(optsBuf), src)
	cmd := fmt.Sprintf("remotefs extractarchive %s", dst)
	process, err := l.currentSVM.config.RunProcess(cmd, input, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to extract archive to %s: %s", dst, err)
	}
	process.Close()

	// Sync the file system to ensure data has been written to disk
	process, err = l.currentSVM.config.RunProcess("sync", nil, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to sync %s after extracting: %s", dst, err)
	}
	process.Close()

	return nil
}

func (l *lcowfs) ArchivePath(src string, opts *archive.TarOptions) (io.ReadCloser, error) {
	logrus.Debugf("REMOTEFS: archive path %s %+v", src, opts)

	optsBuf, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshall tar opts: %s", err)
	}

	// Need to send size first, so that the json package knowns when to stop reading.
	optsSize := uint64(len(optsBuf))
	optsSizeBuf := &bytes.Buffer{}
	if err := binary.Write(optsSizeBuf, binary.BigEndian, optsSize); err != nil {
		return nil, fmt.Errorf("failed to marshall tar opts size: %s", err)
	}

	input := io.MultiReader(optsSizeBuf, bytes.NewBuffer(optsBuf))

	r, w := io.Pipe()
	go func() {
		defer w.Close()
		cmd := fmt.Sprintf("remotefs archivepath %s", src)
		process, err := l.currentSVM.config.RunProcess(cmd, input, w, nil)
		if err != nil {
			logrus.Debugf("REMOTEFS: Failed to extract archive: %s %+v %s", src, opts, err)
		}
		process.Close()
	}()
	return r, nil
}
