// +build windows

package lcow

import (
	"os"
	"runtime"

	"fmt"

	"strconv"

	"github.com/containerd/continuity/driver"
)

var _ driver.Driver = &lcowfs{}

func panicNotImplemented() {
	pc, _, _, _ := runtime.Caller(1)
	name := runtime.FuncForPC(pc).Name()
	panic(fmt.Sprintf("not implemented: %s", name))
}

func (l *lcowfs) Open(p string) (driver.File, error) {
	panicNotImplemented()
	return nil, nil
}

func (l *lcowfs) OpenFile(path string, flag int, perm os.FileMode) (driver.File, error) {
	panicNotImplemented()
	return nil, nil
}

func (l *lcowfs) Readlink(p string) (string, error) {
	panicNotImplemented()
	return "", nil
}

func (l *lcowfs) Mkdir(p string, mode os.FileMode) error {
	panicNotImplemented()
	return nil
}

func (l *lcowfs) Remove(path string) error {
	panicNotImplemented()
	return nil
}

func (l *lcowfs) Link(oldname, newname string) error {
	panicNotImplemented()
	return nil
}

func (l *lcowfs) Lchown(name string, uid, gid int64) error {
	// TODO: error out if uid excesses int bit width?
	panicNotImplemented()
	return nil
}

func (l *lcowfs) Symlink(oldname, newname string) error {
	panicNotImplemented()
	return nil
}

func (l *lcowfs) MkdirAll(path string, perm os.FileMode) error {
	if err := l.startVM(); err != nil {
		return err
	}

	permStr := strconv.FormatUint(uint64(perm), 8)
	cmd := fmt.Sprintf("remotefs mkdirall %s %s", path, permStr)
	process, err := l.currentSVM.config.RunProcess(cmd, nil, nil, nil)
	if err != nil {
		return err
	}
	return process.Close()
}

func (l *lcowfs) RemoveAll(path string) error {
	panicNotImplemented()
	return nil
}

func (l *lcowfs) Mknod(path string, mode os.FileMode, major, minor int) error {
	panicNotImplemented()
	return nil
}

func (l *lcowfs) Mkfifo(path string, mode os.FileMode) error {
	panicNotImplemented()
	return nil
}

// Lchmod changes the mode of an file not following symlinks.
func (l *lcowfs) Lchmod(path string, mode os.FileMode) (err error) {
	panicNotImplemented()
	return nil
}
