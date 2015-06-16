// +build windows

// Shim for Windows Containers Host Compute Service (HSC)

package hcsshim

import (
	"encoding/json"
	"syscall"
	"unsafe"

	log "github.com/Sirupsen/logrus"
)

var (
	modvmcompute = syscall.NewLazyDLL("vmcompute.dll")

	procCreateComputeSystem             = modvmcompute.NewProc("CreateComputeSystem")
	procStartComputeSystem              = modvmcompute.NewProc("StartComputeSystem")
	procCreateProcessInComputeSystem    = modvmcompute.NewProc("CreateProcessInComputeSystem")
	procWaitForProcessInComputeSystem   = modvmcompute.NewProc("WaitForProcessInComputeSystem")
	procShutdownComputeSystem           = modvmcompute.NewProc("ShutdownComputeSystem")
	procTerminateProcessInComputeSystem = modvmcompute.NewProc("TerminateProcessInComputeSystem")
	procResizeConsoleInComputeSystem    = modvmcompute.NewProc("ResizeConsoleInComputeSystem")
)

// The redirection devices as passed in from callers
type Devices struct {
	StdInPipe  string
	StdOutPipe string
	StdErrPipe string
}

// The redirection devices as passed used internally
type deviceInt struct {
	stdinpipe  *uint16
	stdoutpipe *uint16
	stderrpipe *uint16
}

// Configuration will be JSON such as:

//configuration := `{` + "\n"
//configuration += ` "SystemType" : "Container",` + "\n"
//configuration += ` "Name" : "test2",` + "\n"
//configuration += ` "RootDevicePath" : "C:\\Containers\\test",` + "\n"
//configuration += ` "IsDummy" : true` + "\n"
//configuration += `}` + "\n"

// Note that RootDevicePath MUST use \\ not \ as path separator

func CreateComputeSystem(ID string, Configuration string) error {

	log.Debugln("hcsshim::CreateComputeSystem")
	log.Debugln("ID:", ID)
	log.Debugln("Configuration:", Configuration)

	// Convert ID to uint16 pointers for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return err
	}

	// Convert Configuration to uint16 pointers for calling the procedure
	Configurationp, err := syscall.UTF16PtrFromString(Configuration)
	if err != nil {
		log.Debugln("Failed conversion of Configuration to pointer ", err)
		return err
	}

	// Call the procedure itself.
	r1, _, _ := procCreateComputeSystem.Call(
		uintptr(unsafe.Pointer(IDp)), uintptr(unsafe.Pointer(Configurationp)))

	use(unsafe.Pointer(IDp))
	use(unsafe.Pointer(Configurationp))

	if r1 != 0 {
		return syscall.Errno(r1)
	}

	return nil
} // CreateComputeSystem

func StartComputeSystem(ID string) error {

	log.Debugln("hcsshim::StartComputeSystem")
	log.Debugln("ID:", ID)

	// Convert ID to uint16 pointers for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return err
	}

	// Call the procedure itself.
	r1, _, _ := procStartComputeSystem.Call(uintptr(unsafe.Pointer(IDp)))

	use(unsafe.Pointer(IDp))

	if r1 != 0 {
		return syscall.Errno(r1)
	}

	return nil
} // StartComputeSystem

func CreateProcessInComputeSystem(ID string,
	ApplicationName string,
	CommandLine string,
	WorkingDir string,
	StdDevices Devices,
	EmulateTTY uint32) (PID uint32, err error) {

	log.Debugln("hcsshim::CreateProcessInComputeSystem")
	log.Debugln("ID:", ID)
	log.Debugln("CommandLine:", CommandLine)

	// Convert ID to uint16 pointer for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return 0, err
	}

	type processParameters struct {
		ApplicationName, CommandLine, WorkingDirectory string
		StdInPipe, StdOutPipe, StdErrPipe              string
		EmulateConsole                                 bool
	}

	params := &processParameters{
		ApplicationName:  ApplicationName,
		CommandLine:      CommandLine,
		WorkingDirectory: WorkingDir,
		StdInPipe:        StdDevices.StdInPipe,
		StdOutPipe:       StdDevices.StdOutPipe,
		StdErrPipe:       StdDevices.StdErrPipe,
		EmulateConsole:   EmulateTTY != 0,
	}

	paramsJson, err := json.Marshal(params)
	if err != nil {
		return 0, err
	}

	paramsJsonp, err := syscall.UTF16PtrFromString(string(paramsJson))
	if err != nil {
		return 0, err
	}

	// To get a POINTER to the PID
	pid := new(uint32)

	log.Debugln("Calling the procedure itself")

	// Call the procedure itself.
	r1, _, _ := procCreateProcessInComputeSystem.Call(
		uintptr(unsafe.Pointer(IDp)),
		uintptr(unsafe.Pointer(paramsJsonp)),
		uintptr(unsafe.Pointer(pid)))

	use(unsafe.Pointer(IDp))
	use(unsafe.Pointer(paramsJsonp))

	log.Debugln("Returned from procedure call")

	if r1 != 0 {
		return 0, syscall.Errno(r1)
	}

	log.Debugln("hcsshim::CreateProcessInComputeSystem PID ", *pid)
	return *pid, nil
} // CreateProcessInComputeSystem

func WaitForProcessInComputeSystem(ID string, ProcessId uint32) (ExitCode uint32, err error) {

	log.Debugln("hcsshim::WaitForProcessInComputeSystem")
	log.Debugln("ID:", ID)
	log.Debugln("ProcessID:", ProcessId)

	var (
		// Infinite
		Timeout uint32 = 0xFFFFFFFF // (-1)
	)

	// Convert ID to uint16 pointer for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return 0, err
	}

	// To get a POINTER to the ExitCode
	ec := new(uint32)

	// Call the procedure itself.
	r1, _, err := procWaitForProcessInComputeSystem.Call(
		uintptr(unsafe.Pointer(IDp)),
		uintptr(ProcessId),
		uintptr(Timeout),
		uintptr(unsafe.Pointer(ec)))

	use(unsafe.Pointer(IDp))

	if r1 != 0 {
		return 0, syscall.Errno(r1)
	}

	log.Debugln("hcsshim::WaitForProcessInComputeSystem ExitCode ", *ec)
	return *ec, nil
} // WaitForProcessInComputeSystem

func TerminateProcessInComputeSystem(ID string, ProcessId uint32) (err error) {

	log.Debugln("hcsshim::TerminateProcessInComputeSystem")
	log.Debugln("ID:", ID)
	log.Debugln("ProcessID:", ProcessId)

	// Convert ID to uint16 pointer for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return err
	}

	// Call the procedure itself.
	r1, _, err := procTerminateProcessInComputeSystem.Call(
		uintptr(unsafe.Pointer(IDp)),
		uintptr(ProcessId))

	use(unsafe.Pointer(IDp))

	if r1 != 0 {
		return syscall.Errno(r1)
	}

	return nil
} // TerminateProcessInComputeSystem

func ShutdownComputeSystem(ID string) error {

	log.Debugln("hcsshim::ShutdownComputeSystem")
	log.Debugln("ID:", ID)

	// Convert ID to uint16 pointers for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return err
	}

	timeout := uint32(0xffffffff)

	// Call the procedure itself.
	r1, _, err := procShutdownComputeSystem.Call(
		uintptr(unsafe.Pointer(IDp)), uintptr(timeout))

	use(unsafe.Pointer(IDp))

	if r1 != 0 {
		return syscall.Errno(r1)
	}

	return nil
} // ShutdownComputeSystem

func ResizeTTY(ID string, ProcessId uint32, h, w int) error {
	log.Debugf("hcsshim::ResizeTTY %s:%d (%d,%d)", ID, ProcessId, h, w)

	// Make sure ResizeConsoleInComputeSystem is supported
	err := procResizeConsoleInComputeSystem.Find()
	if err != nil {
		return err
	}

	// Convert ID to uint16 pointers for calling the procedure
	IDp, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		log.Debugln("Failed conversion of ID to pointer ", err)
		return err
	}

	h16 := uint16(h)
	w16 := uint16(w)

	r1, _, _ := procResizeConsoleInComputeSystem.Call(uintptr(unsafe.Pointer(IDp)), uintptr(ProcessId), uintptr(h16), uintptr(w16), uintptr(0))
	if r1 != 0 {
		return syscall.Errno(r1)
	}

	return nil
}

// use is a no-op, but the compiler cannot see that it is.
// Calling use(p) ensures that p is kept live until that point.
//go:noescape
func use(p unsafe.Pointer) {}
