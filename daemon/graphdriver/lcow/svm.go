// +build windows

package lcow

// Functions for connecting to a Service VM to support LCOW (Linux Containers on Windows)

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
	"github.com/rneugeba/virtsock/pkg/hvsock"
)

const (
	cmdImport = iota
	cmdExport
	cmdCreateSandbox
	cmdExportSandbox
	cmdTerminate
	cmdResponseOK
	cmdResponseFail

	version1 = iota
	version2

	serviceVMHeaderSize   = 16
	scsiCodeHeaderSize    = 8
	sandboxInfoHeaderSize = 4
	connTimeOut           = 300
	layerVHDName          = "layer.vhd"
	layerSandboxName      = "sandbox.vhdx"
	serviceVMName         = "LinuxServiceVM"
	socketID              = "E9447876-BA98-444F-8C14-6A2FFF773E87"
)

type serviceVMHeader struct {
	Command     uint32
	Version     uint32
	PayloadSize int64
}

type scsiCodeHeader struct {
	controllerNumber   uint32
	controllerLocation uint32
}
type sandboxInfoHeader struct {
	maxSandboxSizeInMB uint32
}

var (
	serviceVMId          hvsock.GUID
	serviceVMSocketID, _ = hvsock.GUIDFromString(socketID)
)

func init() {
	// TODO @jhowardmsft. Will require revisiting.  Get ID for hvsock. For now,
	// assume that it is always up. So, ignore the err for now.
	cmd := fmt.Sprintf("$(Get-ComputeProcess %s).Id", serviceVMName)
	result, _ := exec.Command("powershell", cmd).Output()
	serviceVMId, _ = hvsock.GUIDFromString(strings.TrimSpace(string(result)))
	logrus.Debugf("LCOW graphdriver: serviceVMID %s", serviceVMId)
}

// Process contains information to start a specific application inside the container.
type Process hcsshim.Process

var CurrentTaskProcess hcsshim.Process
var ServiceVMContainer hcsshim.Container

//------------------------ Exported functions --------------------------

func CreateLinuxServiceVM(containerID string) (hcsshim.Container, error) {
	logrus.Infof("[graphdriver::CreateLinuxServiceVM] Start creating LinuxServiceVM (%s)", containerID)

	// prepare configuration for ComputeSystem
	configuration := &hcsshim.ContainerConfig{
		HvPartition:                 true,
		Name:                        containerID,
		SystemType:                  "Container",
		ContainerType:               "Linux",
		Servicing:                   true, // for notifiy the GCS that it's serving the Linux Service Vm
		TerminateOnLastHandleClosed: true,
	}

	// The HCS hardcoded the sandbox name to be sandbox.vhdx
	// we can only specify LayerFolderPath. For ServiceVM,
	// we need separate sandbox file.
	// eg: configuration.LayerFolderPath = "C:\\Linux\\sandbox"
	configuration.LayerFolderPath = "C:\\Linux\\ServiceVM"

	// Setup layers, a list of storage layers.
	// A dummy layer is required the Linux Service VM
	// Format ID=GUID;Path=%root%\windowsfilter\layerID
	configuration.Layers = append(configuration.Layers, hcsshim.Layer{
		ID:   "11111111-2222-2222-3333-567891234567",
		Path: "C:\\Linux\\Layers\\Layer1.vhdx"})
	//logrus.Infof("No c:Linux:Layers provided")

	// boot from initrd
	logrus.Infof("booting from initrd (%s)", containerID)
	configuration.HvRuntime = &hcsshim.HvRuntime{ImagePath: "C:\\Linux\\Kernel", EnableConsole: true}
	/*
		    // Settings for booting from VHD
			//vhdfile := "C:\\Linux\\Kernel\\LinuxServiceVM.vhdx"
			vhdfile := "C:\\Linux\\Kernel\\LCOWBaseOSImage.vhdx"
			logrus.Infof("LinuxServiceVM booting from %s", vhdfile)

			configuration.HvRuntime = &hcsshim.HvRuntime{ImagePath: vhdfile,
		                                                 EnableConsole: true,
		                                                 LayersUseVPMEM:  false,
		                                                 BootSource:  "Vhd",
		                                                 WritableBootSource: true}
	*/

	logrus.Infof("configuration={%s} ServiceVMContainer 0x%0x", configuration, ServiceVMContainer)
	svmContainer, err := hcsshim.CreateContainer(containerID, configuration)
	if err != nil {
		return nil, err
	}
	logrus.Infof("hcsshim.CreateContainer %s succeeded. ServiceVMContainer 0x%0x ", containerID, ServiceVMContainer)

	err = svmContainer.Start()
	if err != nil {
		return nil, err
	}

	ServiceVMContainer = svmContainer
	logrus.Infof("LinuxServiceVM hcsContainer.Start(id=%s) succeeded and CreateLinuxServiceVM completed successfully", containerID)
	return nil, nil
}

func DeleteLinuxServiceVM(containerID string) error {
	logrus.Infof("[graphdriver] Deleting LinuxServiceVM")

	// Shutdown will do a clean shutdown, send a message to the GCS,
	// and is valid only after a successful Start.
	// If Shutdown returns successfully, the compute system is completely
	// cleaned up and no further action is needed.
	err := ServiceVMContainer.Shutdown()
	if err != nil {
		return err
	}

	// Call Terminiate() if a *successful* Start has not been performed.
	// Terminate can be called at any time, but it will not communicate with the GCS, the VM is killed.
	//err = ServiceVMContainer.Terminate();
	return nil
}

// Convert a tar stream, coming fom the "reader", into a fixed vhd file
func importLayer(layerPath string, reader io.Reader) (int64, error) {
	logrus.Infof("[ServiceVMImportLayer] Calling tar2vhdName in ServiceVM for converting %s to a vhd file", layerPath)

	// Execute tar_to_vhd as a external process in the ServiceVM for
	// converting a tar into a fixed VHD file
	process, err := launchProcessInServiceVM("tar2vhd")
	if err != nil {
		logrus.Errorf("launchProcessInServiceVM failed with %s", err)
		return 0, err
	}
	defer process.Close()
	CurrentTaskProcess = process

	// get the std io pipes from the newly created process
	stdin, stdout, _, err := process.Stdio()
	if err != nil {
		logrus.Errorf("[ServiceVMImportLayer]  getting std pipes failed %s", err)
		return 0, err
	}

	logrus.Infof("[ServiceVMImportLayer] Sending the tar stream to the LinuxServiceVM")
	_, err = io.Copy(stdin, reader)
	if err != nil {
		logrus.Infof("[ServiceVMImportLayer] Failed to send data over Linux service vm")
		return 0, err
	}
	process.CloseStdin()

	logrus.Infof("[ServiceVMImportLayer] reading back vhd stream")
	// We are getting the VHD stream, so write it to file
	payloadSize, err := writeVHDFile(path.Join(layerPath, layerVHDName), stdout)
	if err != nil {
		return 0, err
	}
	logrus.Infof("[ServiceVMImportLayer] wrote (%d bytes) to VHD", payloadSize)
	return payloadSize, err
}

// exportLayer exports a sandbox layer
func exportLayer(vhdPath string) (io.ReadCloser, error) {
	logrus.Debugf("exportLayer vhdPath %s", vhdPath)
	// Check if sandbox
	if _, err := os.Stat(filepath.Join(vhdPath, layerSandboxName)); err == nil {
		logrus.Debugf("exportLayer is a sandbox")
		return exportSandbox(vhdPath)
	}

	// Otherwise, it's a normal vhd file.
	logrus.Debugf("exportLayer is a normal VHD ")
	vhdFile, err := os.Open(path.Join(vhdPath, layerVHDName))
	if err != nil {
		return nil, err
	}
	defer vhdFile.Close()

	// Execute tar_to_vhd as a external process in the ServiceVM for
	// converting a tar into a fixed VHD file
	process, err := launchProcessInServiceVM("vhd2tar")
	if err != nil {
		logrus.Errorf("launchProcessInServiceVM failed with %s", err)
		return nil, err
	}
	defer process.Close()

	// get the std io pipes from the newly created process
	stdin, stdout, _, err := process.Stdio()
	if err != nil {
		logrus.Errorf("[ServiceVMExportLayer]  getting std pipes failed %s", err)
		return nil, err
	}

	_, err = io.Copy(stdin, vhdFile)
	if err != nil {
		logrus.Errorf("[ServiceVMExportLayer] sending data failed %s", err)
		return nil, err
	}
	process.CloseStdin()

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		//defer sendClose(stdout)
		logrus.Infof("Copying result over hvsock")
		io.Copy(writer, stdout)
	}()
	return reader, nil
}

// Create a sandbox file named LayerSandboxName under sandboxFolder on the host
// This is donee by copying a prebuilt-sandbox from the ServiceVM
//
func createSandbox(sandboxFolder string) error {
	sandboxPath := path.Join(sandboxFolder, layerSandboxName)
	fmt.Printf("ServiceVMCreateSandbox: Creating sandbox path: %s\n", sandboxPath)

	// launch a process in the ServiceVM for handling the sandbox creation
	process, err := launchProcessInServiceVM("createSandbox")
	if err != nil {
		logrus.Infof("launchProcessInServiceVM failed with %s", err)
		return err
	}
	defer process.Close()

	// get the std io pipes from the newly created process
	_, stdout, _, err := process.Stdio()
	if err != nil {
		logrus.Errorf("[ServiceVMCreateSandbox] getting std pipes from the newly created process failed %s", err)
		return err
	}

	logrus.Infof("writing vhdx stream to file")
	// Get back the sandbox VHDx stream from the service VM and write it to file
	_, err = writeVHDFile(sandboxPath, stdout)
	if err != nil {
		return err
	}

	fmt.Printf("[ServiceVMCreateSandbox]: done creating %s\n", sandboxPath)
	return err
}

//----------------------------- internal utility routines ------------------------

func launchProcessInServiceVM(commandline string) (Process, error) {

	logrus.Infof("launchProcessInServiceVM :[%s]", commandline)

	createProcessParms := hcsshim.ProcessConfig{
		EmulateConsole:    false,
		CreateStdInPipe:   true,
		CreateStdOutPipe:  true,
		CreateStdErrPipe:  true,
		CreateInUtilityVm: true,
	}

	// Temporary setting root as working directory
	createProcessParms.WorkingDirectory = "/root/integration"

	// Configure the environment for the process
	//pathValue := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/mnt/gcs/LinuxServiceVM/scratch/bin"
	pathValue := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/root/integration"
	createProcessParms.Environment = map[string]string{"PATH": pathValue}

	createProcessParms.CommandLine = commandline
	createProcessParms.User = "" // what to put here? procToAdd.User.Username
	logrus.Debugf("before CreateProcess:commandLine: %s", createProcessParms.CommandLine)

	// Start the command running in the service VM.
	newProcess, err := ServiceVMContainer.CreateProcess(&createProcessParms)
	if err != nil {
		logrus.Errorf("launchProcessInServiceVM: CreateProcess() failed %s", err)
		return nil, err
	}
	logrus.Debugf("after CreateProcess: %s", createProcessParms.CommandLine)

	pid := newProcess.Pid()
	logrus.Infof("newProcess id is 0x%0x", pid)

	// TO DO: when to cleanup
	// need to find a place to call Close on newProcess hcsProcess.Close()
	// Spin up a go routine waiting for exit to handle cleanup
	//go container.waitExit(proc, false)

	return newProcess, nil
}

func storeReader(r io.Reader) (*os.File, int64, error) {
	tmpFile, err := ioutil.TempFile("", "docker-reader")
	if err != nil {
		return nil, 0, err
	}

	fileSize, err := io.Copy(tmpFile, r)
	if err != nil {
		return nil, 0, err
	}

	_, err = tmpFile.Seek(0, 0)
	if err != nil {
		return nil, 0, err
	}
	return tmpFile, fileSize, nil
}

func waitForResponse(r io.Reader) (int64, error) {
	buf := make([]byte, serviceVMHeaderSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		logrus.Infof("[waitForResponse] io.ReadFull failed with %s", err)
		return 0, err
	}

	hdr, err := deserializeHeader(buf)
	if err != nil {
		logrus.Infof("[waitForResponse] deserializeHeader failed with %s", err)
		return 0, err
	}

	if hdr.Command != cmdResponseOK {
		logrus.Infof("[waitForResponse] hdr.Command = 0x%0x", hdr.Command)
		return 0, fmt.Errorf("Service VM failed")
	}
	return hdr.PayloadSize, nil
}

func writeVHDFile(path string, r io.Reader) (int64, error) {
	fmt.Println(path)

	f, err := os.Create(path)
	if err != nil {
		fmt.Errorf("os.Create(%s) failed %s", path, err)
		return 0, err
	}
	defer f.Close()

	n, err := io.Copy(f, r)
	if err != nil {
		fmt.Errorf("io.CopyN failed %s", err)
		return 0, err
	}
	return n, nil
}

func newVHDX(pathName string) error {
	return exec.Command("powershell",
		"New-VHD",
		"-Path", pathName,
		"-Dynamic",
		"-BlockSizeBytes", "1MB",
		"-SizeBytes", "16GB").Run()
}

func attachVHDX(pathName string) (uint32, uint32, error) {
	res, err := exec.Command("powershell",
		"Add-VMHardDiskDrive",
		"-Path", pathName,
		"-VMName", serviceVMName,
		"-Passthru").Output()

	if err != nil {
		return 0, 0, err
	}

	re := regexp.MustCompile("SCSI *[0-9]+ *[0-9]+")
	resultStr := re.FindString(string(res))
	fields := strings.Fields(resultStr)
	if len(fields) != 3 {
		return 0, 0, fmt.Errorf("Error invalid disk attached to service VM")
	}

	controllerNumber, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return 0, 0, err
	}

	controllerLocation, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return 0, 0, err
	}
	return uint32(controllerNumber), uint32(controllerLocation), nil
}

func detachVHDX(controllerNum, controllerLoc uint32) error {
	cn := strconv.FormatUint(uint64(controllerNum), 10)
	cl := strconv.FormatUint(uint64(controllerLoc), 10)
	err := exec.Command("powershell",
		"Remove-VMHardDiskDrive",
		"-ControllerType", "SCSI",
		"-ControllerNumber", cn,
		"-ControllerLocation", cl,
		"-VMName", serviceVMName).Run()
	return err
}

func serializeSCSI(header *serviceVMHeader, scsiHeader *scsiCodeHeader) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, header); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.BigEndian, scsiHeader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportSandbox(sandboxFolder string) (io.ReadCloser, error) {
	sandboxPath := path.Join(sandboxFolder, layerSandboxName)
	fmt.Printf("ServiceVMAttachSandbox: Creating sandbox path: %s\n", sandboxPath)

	controllerNumber, controllerLocation, err := attachVHDX(sandboxPath)
	if err != nil {
		return nil, err
	}
	defer detachVHDX(controllerNumber, controllerLocation)
	fmt.Printf("ServiceVMExportSandbox: Got Controller number: %d controllerLocation: %d\n", controllerNumber, controllerLocation)

	hdr := &serviceVMHeader{
		Command:     cmdExportSandbox,
		Version:     version1,
		PayloadSize: scsiCodeHeaderSize,
	}

	scsiHeader := &scsiCodeHeader{
		controllerNumber:   controllerNumber,
		controllerLocation: controllerLocation,
	}

	data, err := serializeSCSI(hdr, scsiHeader)
	if err != nil {
		return nil, err
	}

	conn, err := connect()
	if err != nil {
		return nil, err
	}

	fmt.Println("EXPORTING SANDBOX TO TAR", data)
	_, err = conn.Write(data)
	if err != nil {
		return nil, err
	}

	payloadSize, err := waitForResponse(conn)
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
		fmt.Println("Copying result over hvsock")
		io.CopyN(writer, conn, payloadSize)
		closeConnection(conn)
		writer.Close()
	}()
	return reader, nil
}

func sendData(hdr *serviceVMHeader, payload io.Reader, dest io.Writer) error {
	hdrBytes, err := serializeHeader(hdr)
	if err != nil {
		return err
	}
	logrus.Infof("[SendData] Total bytes to send %d", hdr.PayloadSize)

	_, err = dest.Write(hdrBytes)
	if err != nil {
		return err
	}

	// break into 4Kb chunks
	var max_transfer_size int64
	var bytes_to_transfer int64
	var total_bytes_transfered int64

	bytes_left := hdr.PayloadSize
	//max_transfer_size = 4096
	max_transfer_size = bytes_left

	total_bytes_transfered = 0
	bytes_to_transfer = 0

	for bytes_left > 0 {
		if bytes_left >= max_transfer_size {
			bytes_to_transfer = max_transfer_size
		} else {
			bytes_to_transfer = bytes_left
		}

		bytes_transfered, err := io.CopyN(dest, payload, bytes_to_transfer)
		if err != nil && err != io.EOF {
			logrus.Errorf("[SendData] io.Copy failed with %s", err)
			return err
		}
		logrus.Infof("[SendData] bytes_transfered = %d bytes", bytes_transfered)

		total_bytes_transfered += bytes_transfered
		bytes_left -= bytes_transfered
	}
	logrus.Infof("[SendData] total_bytes_transfered = %d bytes sent to the LinuxServiceVM successfully", total_bytes_transfered)
	return err
}

func readHeader(r io.Reader) (*serviceVMHeader, error) {
	hdr := &serviceVMHeader{}
	buf, err := serializeHeader(hdr)
	if err != nil {
		return nil, err
	}

	_, err = io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}

	return deserializeHeader(buf)
}

func serializeHeader(hdr *serviceVMHeader) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func deserializeHeader(hdr []byte) (*serviceVMHeader, error) {
	buf := bytes.NewBuffer(hdr)
	hdrPtr := &serviceVMHeader{}
	if err := binary.Read(buf, binary.BigEndian, hdrPtr); err != nil {
		return nil, err
	}
	return hdrPtr, nil
}

func connect() (hvsock.Conn, error) {
	hvAddr := hvsock.HypervAddr{VMID: serviceVMId, ServiceID: serviceVMSocketID}
	return hvsock.Dial(hvAddr)
}

func closeConnection(rc io.WriteCloser) error {
	header := &serviceVMHeader{
		Command:     cmdTerminate,
		Version:     version1,
		PayloadSize: 0,
	}

	buf, err := serializeHeader(header)
	if err != nil {
		rc.Close()
		return err
	}

	_, err = rc.Write(buf)
	if err != nil {
		rc.Close()
		return err
	}
	return rc.Close()
}
