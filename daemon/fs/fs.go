package fs

import (
	"io"

	"os"

	"github.com/docker/docker/pkg/archive"
)

// FilesystemOperator is an interface that provides ways to interact with a container's root file system.
type FilesystemOperator interface {
	// Remote returns true if the file system is remote. false otherwise.
	Remote() bool

	// HostPathName returns the path where the file system is mounted.
	// If the file system is remote, then it will be the path on the
	// remote system.
	HostPathName() string

	// ResolveFullPath resolves the given path as an absolute path on the target machine.
	ResolveFullPath(name string) (string, error)

	// Platform returns the OS of the remote filesystem
	// TODO @gupta-ak. Remove this later since John Howard has a Platform field in the
	// container.
	Platform() string

	// ExtractArchive takes in an archive and extracts it to the given path.
	ExtractArchive(input io.Reader, path string, options *archive.TarOptions) error

	// ArchivePath archives the given path (file or directory) and returns
	// the archive.
	ArchivePath(path string, options *archive.TarOptions) (io.ReadCloser, error)

	// The following functions are from the go os package, ioutil & syscall package
	Stat(name string) (os.FileInfo, error)
	Lstat(name string) (os.FileInfo, error)

	//ReadFile(filename string) ([]byte, error)
	//WriteFile(filename string, data []byte, perm os.FileMode) error
	//ReadDir(dirname string) ([]os.FileInfo, error)

	//Mkdir(name string, perm FileMode) error
	//MkdirAll(path string, perm os.FileMode) error
	//Remove(name string) error
	//RemoveAll(path string) error
	//Rename(oldpath, newpath string) error

	//Link(oldname, newname string) error
	//Symlink(oldname, newname string) error
	//Readlink(name string) (string, error)

	//Chmod(name string, mode os.FileMode) error
	//Chown(name string, uid, gid int) error
	//Chtimes(name string, atime time.Time, mtime time.Time) error

	//Lchmod(name string, mode os.FileMode) error
	//Lchown(name string, uid, gid int) error
	//Lchtimes(name string, atime time.Time, mtime time.Time) error

	//Mount(source string, target string, fstype string, flags uintptr, data string) (err error)
}

// NewFilesystemOperator returns a remote or local filesystem operator
func NewFilesystemOperator(isRemote bool, hostPath string) FilesystemOperator {
	if isRemote {
		return &remotefs{dummyPath: hostPath}
	}
	return &localfs{root: hostPath}
}
