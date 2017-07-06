package lcow

import (
	"os"
	"runtime"

	"fmt"

	"github.com/containerd/continuity/driver"
)

var _ driver.Driver = &lcowfs{}

func panicNotImplemented() {
	pc, _, _, _ := runtime.Caller(1)
	name := runtime.FuncForPC(pc).Name()
	panic(fmt.Sprintf("not implemented: %s", name))
}

func (d *lcowfs) Open(p string) (driver.File, error) {
	panicNotImplemented()
	return nil, nil
}

func (d *lcowfs) OpenFile(path string, flag int, perm os.FileMode) (driver.File, error) {
	panicNotImplemented()
	return nil, nil
}

func (d *lcowfs) Readlink(p string) (string, error) {
	panicNotImplemented()
	return "", nil
}

func (d *lcowfs) Mkdir(p string, mode os.FileMode) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) Remove(path string) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) Link(oldname, newname string) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) Lchown(name string, uid, gid int64) error {
	// TODO: error out if uid excesses int bit width?
	panicNotImplemented()
	return nil
}

func (d *lcowfs) Symlink(oldname, newname string) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) MkdirAll(path string, perm os.FileMode) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) RemoveAll(path string) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) Mknod(path string, mode os.FileMode, major, minor int) error {
	panicNotImplemented()
	return nil
}

func (d *lcowfs) Mkfifo(path string, mode os.FileMode) error {
	panicNotImplemented()
	return nil
}

// Lchmod changes the mode of an file not following symlinks.
func (d *lcowfs) Lchmod(path string, mode os.FileMode) (err error) {
	panicNotImplemented()
	return nil
}
