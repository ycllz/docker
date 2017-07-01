package lcow

import (
	"fmt"
	"io"

	"bytes"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/chrootarchive"
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
	process, err := l.currentSVM.config.RunProcess(cmd, nil, output, nil)
	if err != nil {
		return "", err
	}
	process.Close()

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
