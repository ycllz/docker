// +build windows

package client

// TODO @jhowardmsft - This will move to Microsoft/opengcs soon

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"

	"io/ioutil"

	"github.com/Sirupsen/logrus"
)

var (
	modkernel32   = syscall.NewLazyDLL("kernel32.dll")
	procCopyFileW = modkernel32.NewProc("CopyFileW")
)

// writeFileFromReader writes an output file from an io.Reader
func writeFileFromReader(path string, reader io.Reader, timeoutSeconds int, context string) (int64, error) {
	outFile, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("opengcs: writeFileFromReader: failed to create %s: %s", path, err)
	}
	defer outFile.Close()
	return copyWithTimeout(outFile, reader, 0, timeoutSeconds, context)
}

// copyWithTimeout is a wrapper for io.Copy using a timeout duration
func copyWithTimeout(dst io.Writer, src io.Reader, size int64, timeoutSeconds int, context string) (int64, error) {
	logrus.Debugf("opengcs: copywithtimeout: size %d: timeout %d: (%s)", size, timeoutSeconds, context)

	type resultType struct {
		err   error
		bytes int64
	}

	done := make(chan resultType, 1)
	go func() {
		// TODO @jhowardmsft. Needs platform fix. Improve reliability by
		// chunking the data. Ultimately can just use io.Copy instead with no loop
		result := resultType{}
		var copied int64
		for {
			copied, result.err = io.CopyN(dst, src, 1024)
			result.bytes += copied
			if copied == 0 {
				done <- result
				break
			}
			// TODO @jhowardmsft - next line is debugging only. Remove
			//logrus.Debugf("%s: copied so far %d\n", context, result.bytes)
		}
	}()

	var result resultType
	timedout := time.After(time.Duration(timeoutSeconds) * time.Second)

	select {
	case <-timedout:
		return 0, fmt.Errorf("opengcs: copyWithTimeout: timed out (%s)", context)
	case result = <-done:
		if result.err != nil && result.err != io.EOF {
			// See https://github.com/golang/go/blob/f3f29d1dea525f48995c1693c609f5e67c046893/src/os/exec/exec_windows.go for a clue as to why we are doing this :)
			if se, ok := result.err.(syscall.Errno); ok {
				const (
					errNoData     = syscall.Errno(232)
					errBrokenPipe = syscall.Errno(109)
				)
				if se == errNoData || se == errBrokenPipe {
					logrus.Debugf("opengcs: copyWithTimeout: hit NoData or BrokenPipe: %d: %s", se, context)
					return result.bytes, nil
				}
			}
			return 0, fmt.Errorf("opengcs: copyWithTimeout: error reading: '%s' after %d bytes (%s)", result.err, result.bytes, context)
		}
	}
	logrus.Debugf("opengcs: copyWithTimeout: success - copied %d bytes (%s)", result.bytes, context)
	return result.bytes, nil
}

// copyFile is a utility for copying a file - used for the sandbox cache.
// Uses CopyFileW win32 API for performance
func copyFile(srcFile, destFile string) error {
	var bFailIfExists uint32 = 1

	lpExistingFileName, err := syscall.UTF16PtrFromString(srcFile)
	if err != nil {
		return err
	}
	lpNewFileName, err := syscall.UTF16PtrFromString(destFile)
	if err != nil {
		return err
	}
	r1, _, err := syscall.Syscall(
		procCopyFileW.Addr(),
		3,
		uintptr(unsafe.Pointer(lpExistingFileName)),
		uintptr(unsafe.Pointer(lpNewFileName)),
		uintptr(bFailIfExists))
	if r1 == 0 {
		return fmt.Errorf("failed CopyFileW Win32 call from '%s' to %s: %s", srcFile, destFile, err)
	}
	return nil

}

func getTarSize(r io.Reader) (*os.File, int64, error) {
	file, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, 0, err
	}

	size, err := io.Copy(file, r)
	if err != nil {
		return nil, 0, err
	}

	_, err = file.Seek(0, 0)
	if err != nil {
		return nil, 0, err
	}

	return file, size, nil
}
