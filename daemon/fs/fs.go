package fs

import (
	"io"

	"github.com/containerd/continuity/fsdriver"
	"github.com/docker/docker/pkg/archive"
)

// FilesystemOperator is an interface that provides ways to interact with a container's root file system.
type FilesystemOperator interface {
	// Remote returns true if the file system is remote. false otherwise.
	// TODO @gupta-ak. This probably isn't needed & can be removed, but keep for now.
	Remote() bool

	// HostPathName returns the path where the file system is mounted.
	// If the file system is remote, then it will be the path on the
	// remote system.
	HostPathName() string

	// ResolveFullPath resolves the given path as an absolute path on the target machine.
	ResolveFullPath(name string) (string, error)

	// ExtractArchive takes in an archive and extracts it to the given path.
	ExtractArchive(input io.Reader, path string, options *archive.TarOptions) error

	// ArchivePath archives the given path (file or directory) and returns
	// the archive.
	ArchivePath(path string, options *archive.TarOptions) (io.ReadCloser, error)

	// Driver has the os system calls and path manipulation operations
	fsdriver.Driver
}

// NewFilesystemOperator returns a remote or local filesystem operator
func NewFilesystemOperator(driverType fsdriver.DriverType, hostPath string) (FilesystemOperator, error) {
	driver, err := fsdriver.NewSystemDriver(driverType)
	if err != nil {
		return nil, err
	}

	if driverType == fsdriver.LOW {
		return &remotefs{root: hostPath, Driver: driver}, nil
	}
	return &localfs{root: hostPath, Driver: driver}, nil
}
