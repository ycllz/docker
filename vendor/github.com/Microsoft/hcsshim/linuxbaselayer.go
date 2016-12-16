package hcsshim

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"

	winio "github.com/Microsoft/go-winio"
	winlx "github.com/Microsoft/go-winlx"
)

type baseLinuxLayerWriter struct {
	root string
	h    syscall.Handle
	err  error
}

func ntMakeLongAbsPath(path string) (string, error) {
	p, err := makeLongAbsPath(path)
	if err != nil {
		return "", err
	}

	// nt paths are actually \??\ not \\.\ or \\?\
	pb := []byte(p)
	pb[1] = '?'
	pb[2] = '?'
	return string(pb), nil
}

// TODO: Add NtStatus -> Errno translation
func (w *baseLinuxLayerWriter) closeCurrentFile() error {
	if w.h != 0 {
		err := winlx.LxClose(w.h)
		w.h = 0
		if err < 0 {
			return ERROR_GEN_FAILURE
		}
	}
	return nil
}

func (w *baseLinuxLayerWriter) Add(name string, fileFullInfo *winio.FileFullInfo) (err error) {
	defer func() {
		if err != nil {
			w.err = err
		}
	}()

	err = w.closeCurrentFile()
	if err != nil {
		return err
	}

	// Create unix -> windows path
	path, err := ntMakeLongAbsPath(filepath.Join(w.root, name))
	if err != nil {
		return err
	}

	fmt.Println(path)
	fmt.Printf("Uid: %d Gid: %d Mode %o\n", fileFullInfo.LinuxInfo.UID, fileFullInfo.LinuxInfo.GID, fileFullInfo.LinuxInfo.Mode)
	fmt.Printf("Handle should be closed: %d\n", w.h)

	// Now create the file.
	var handle syscall.Handle
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	createInfo := winlx.CreateInfo{
		Uid:   uint32(fileFullInfo.LinuxInfo.UID),
		Gid:   uint32(fileFullInfo.LinuxInfo.GID),
		DevId: 0,
		Time:  0,
	}
	status := winlx.LxCreat(pathPtr,
		uint32(fileFullInfo.LinuxInfo.Mode),
		&createInfo,
		&handle)
	if status < 0 {
		fmt.Println("FAILED CREATE")
		err = ERROR_GEN_FAILURE
		return err
	}
	fmt.Printf("Opened: %d\n", handle)
	w.h = handle
	return nil
}

func (w *baseLinuxLayerWriter) AddLink(name string, target string) (err error) {
	defer func() {
		if err != nil {
			w.err = err
		}
	}()

	err = w.closeCurrentFile()
	if err != nil {
		return err
	}

	linkpath, err := ntMakeLongAbsPath(filepath.Join(w.root, name))
	if err != nil {
		return err
	}

	linktarget, err := ntMakeLongAbsPath(filepath.Join(w.root, target))
	if err != nil {
		return err
	}

	linkpathPtr, err := syscall.UTF16PtrFromString(linkpath)
	if err != nil {
		return err
	}

	linktargetPtr, err := syscall.UTF16PtrFromString(linktarget)
	if err != nil {
		return err
	}

	fmt.Printf("%s -> %s\n", linkpath, linktarget)

	status := winlx.LxLink(linktargetPtr, linkpathPtr)
	if status < 0 {
		fmt.Println("FAILED LINK")
		err = ERROR_GEN_FAILURE
		return err
	}
	return nil
}

func (w *baseLinuxLayerWriter) Remove(name string) error {
	return errors.New("base layer cannot have tombstones")
}

func (w *baseLinuxLayerWriter) Write(b []byte) (int, error) {
	fmt.Printf("Writing: %d\n", w.h)

	var n uint32
	err := winlx.LxWrite(w.h, b, &n)

	var errReal error
	if err < 0 {
		errReal = ERROR_GEN_FAILURE
	}
	if errReal != nil {
		fmt.Println("FAILED WRITE")
		w.err = errReal
	}
	return int(n), errReal
}

func (w *baseLinuxLayerWriter) Close() error {
	err := w.closeCurrentFile()
	if err != nil {
		return err
	}
	return w.err
}
