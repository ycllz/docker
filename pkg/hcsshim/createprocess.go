// +build windows

package hcsshim

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/Sirupsen/logrus"
)

// The redirection devices as passed in from callers
type Devices struct {
	StdInPipe  string
	StdOutPipe string
	StdErrPipe string
}

// The redirection devices used internally
type deviceInt struct {
	stdinpipep  *uint16
	stdoutpipep *uint16
	stderrpipep *uint16
}

// CreateProcess starts a process in a container. This is invoked, for example,
// as a result of docker run, docker exec, or RUN in Dockerfile. If successful,
// it returns the PID of the process.
func CreateProcessInComputeSystem(id string,
	applicationname string,
	commandline string,
	workingdir string,
	stddevices Devices,
	emulatetty uint32) (processid uint32, err error) {

	title := "HCSShim::CreateProcessInComputeSystem"
	logrus.Debugf(title+"id=%s applicationname=%s commandline=%s workingdir=%s emulatetty=%t in=%s out=%s err=%s",
		id, applicationname, commandline, workingdir, emulatetty, stddevices.StdInPipe, stddevices.StdOutPipe, stddevices.StdErrPipe)

	// Load the DLL and get a handle to the procedure we need
	dll, proc, err := loadAndFind(procCreateProcessInComputeSystem)
	if dll != nil {
		defer dll.Release()
	}
	if err != nil {
		return 0, err
	}

	// Convert id to uint16 pointer for calling the procedure
	idp, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		err = fmt.Errorf(title+" - Failed conversion of id %s to pointer %s", id, err)
		logrus.Error(err)
		return 0, err
	}

	// Convert applicationname to uint16 pointer for calling the procedure
	applicationnamep, err := syscall.UTF16PtrFromString(applicationname)
	if err != nil {
		err = fmt.Errorf(title+" - Failed conversion of applicationname %s to pointer %s", applicationname, err)
		logrus.Error(err)
		return 0, err
	}

	// Convert commandline to uint16 pointer for calling the procedure
	commandlinep, err := syscall.UTF16PtrFromString(commandline)
	if err != nil {
		err = fmt.Errorf(title+" - Failed conversion of commandline %s to pointer %s", commandline, err)
		logrus.Error(err)
		return 0, err
	}

	// Convert workingdir to uint16 pointer for calling the procedure
	workingdirp, err := syscall.UTF16PtrFromString(workingdir)
	if err != nil {
		err = fmt.Errorf(title+" - Failed conversion of workingdir %s to pointer %s", workingdir, err)
		logrus.Error(err)
		return 0, err
	}

	var (
		stdinpipep  *uint16
		stdoutpipep *uint16
		stderrpipep *uint16
	)

	// Convert StdinPipe, if supplied to uint16 pointer for calling the procedure
	if len(stddevices.StdInPipe) != 0 {
		stdinpipep, err = syscall.UTF16PtrFromString(stddevices.StdInPipe)
		if err != nil {
			err = fmt.Errorf(title+" - Failed conversion of StdInPipe %s to pointer %s ", stddevices.StdInPipe, err)
			logrus.Error(err)
			return 0, err
		}
	}

	// Convert StdOutPipe, if supplied to uint16 pointer for calling the procedure
	if len(stddevices.StdOutPipe) != 0 {
		stdoutpipep, err = syscall.UTF16PtrFromString(stddevices.StdOutPipe)
		if err != nil {
			err = fmt.Errorf(title+" - Failed conversion of StdOutPipe %s to pointer %s", stddevices.StdOutPipe, err)
			logrus.Error(err)
			return 0, err
		}
	}

	// Convert StdErrPipe, if supplied to uint16 pointer for calling the procedure
	if len(stddevices.StdErrPipe) != 0 {
		stderrpipep, err = syscall.UTF16PtrFromString(stddevices.StdErrPipe)
		if err != nil {
			err = fmt.Errorf(title+" - Failed conversion of StdErrPipe %s to pointer %s", stddevices.StdErrPipe, err)
			logrus.Error(err)
			return 0, err
		}
	}

	// Build the structure of pipe pointers for calling the HCS
	internaldevices := &deviceInt{
		stdinpipep:  stdinpipep,
		stdoutpipep: stdoutpipep,
		stderrpipep: stderrpipep,
	}

	// Get a POINTER to variable to take the pid outparm
	pid := new(uint32)

	logrus.Debugln(title + " - Calling the procedure itself")

	// Call the procedure itself.
	r1, _, _ := proc.Call(
		uintptr(unsafe.Pointer(idp)),
		uintptr(unsafe.Pointer(applicationnamep)),
		uintptr(unsafe.Pointer(commandlinep)),
		uintptr(unsafe.Pointer(workingdirp)),
		uintptr(0), // TODO: Environment to follow if implemented in HCS
		uintptr(unsafe.Pointer(internaldevices)),
		uintptr(emulatetty),
		uintptr(unsafe.Pointer(pid)))

	use(unsafe.Pointer(internaldevices))
	use(unsafe.Pointer(idp))
	use(unsafe.Pointer(applicationnamep))
	use(unsafe.Pointer(commandlinep))
	use(unsafe.Pointer(workingdirp))

	if r1 != 0 {
		err = fmt.Errorf(title+" - Win32 API call returned error r1=%d errno=%d id=%s applicationname=%s commandline=%s workingdir=%s emulatetty=%t",
			r1, syscall.Errno(r1), id, applicationname, commandline, workingdir, emulatetty)
		logrus.Error(err)
		return 0, err
	}

	logrus.Debugf(title+" - succeeded id=%s pid=%d", id, *pid)
	return *pid, nil
}
