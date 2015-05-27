// +build windows

package hcsshim

import (
	"github.com/Sirupsen/logrus"
)

func ResizeTTY(h, w int) error {

	title := "HCSShim::ResizeTTY"
	logrus.Debugf(title+"(%d,%d) - NOT IMPLEMENTED", h, w)
	return nil
	/*

		// TODO Windows: Needs fully implementing in HCS, along with buffer.
		// Keep this code here for now as a placeholder.


		// Load the DLL and get a handle to the procedure we need
		dll, proc, err := loadAndFind(procResizeTTY)
		if dll != nil {
			defer dll.Release()
		}
		if err != nil {
			return 0, err
		}

		h32 := uint32(h)
		w32 := uint32(w)

		r1, _, _ := proc.Call(uintptr(h32), uintptr(w32))
		if r1 != 0 {
			err = fmt.Errorf("HCSShim::ResizeTTY - Win32 API call returned error", r1, syscall.Errno(r1))
			logrus.Error(err)
			return err
		}

		logrus.Debugln(title+" - succeeded")
		return nil
	*/
}
