package lcow

import (
	"os"

	"github.com/containerd/continuity/driver"
	"github.com/pkg/errors"
)

var _ driver.Driver = &lcowfs{}

func (d *lcowfs) Open(p string) (driver.File, error) {
	return os.Open(p)
}

func (d *lcowfs) OpenFile(path string, flag int, perm os.FileMode) (driver.File, error) {
	return os.OpenFile(path, flag, perm)
}

func (d *lcowfs) Stat(p string) (os.FileInfo, error) {
	return os.Stat(p)
}

func (d *lcowfs) Lstat(p string) (os.FileInfo, error) {
	return os.Lstat(p)
}

func (d *lcowfs) Readlink(p string) (string, error) {
	return os.Readlink(p)
}

func (d *lcowfs) Mkdir(p string, mode os.FileMode) error {
	return os.Mkdir(p, mode)
}

func (d *lcowfs) Remove(path string) error {
	return os.Remove(path)
}

func (d *lcowfs) Link(oldname, newname string) error {
	return os.Link(oldname, newname)
}

func (d *lcowfs) Lchown(name string, uid, gid int64) error {
	// TODO: error out if uid excesses int bit width?
	return os.Lchown(name, int(uid), int(gid))
}

func (d *lcowfs) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

func (d *lcowfs) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (d *lcowfs) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (d *lcowfs) Mknod(path string, mode os.FileMode, major, minor int) error {
	return errors.Wrap(ErrNotSupported, "cannot create device node on Windows")
}

func (d *lcowfs) Mkfifo(path string, mode os.FileMode) error {
	return errors.Wrap(ErrNotSupported, "cannot create fifo on Windows")
}

// Lchmod changes the mode of an file not following symlinks.
func (d *lcowfs) Lchmod(path string, mode os.FileMode) (err error) {
	// TODO: Use Window's equivalent
	return os.Chmod(path, mode)
}
