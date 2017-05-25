package servicevm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
)

// Process contains information to start a specific application inside the container.
type Process hcsshim.Process

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
func ServiceVMImportLayer(layerPath string, reader io.Reader) (int64, error) {
	logrus.Infof("[ServiceVMImportLayer] Calling tar2vhdName in ServiceVM for converting %s to a vhd file", layerPath)
	// copy down the tar stream and store it to a temp file
	// for preparing to write teo stdin pipe into the ServiceVM
	tmpFile, fileSize, err := storeReader(reader)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Execute tar_to_vhd as a external process in the ServiceVM for
	// converting a tar into a fixed VHD file
	process, err := launchProcessInServiceVM("./svm_utils")
	if err != nil {
		logrus.Errorf("launchProcessInServiceVM failed with %s", err)
		return 0, err
	}

	// get the std io pipes from the newly created process
	stdin, stdout, _, err := process.Stdio()
	if err != nil {
		logrus.Errorf("[ServiceVMImportLayer]  getting std pipes failed %s", err)
		return 0, err
	}

	header := &ServiceVMHeader{
		Command:     ImportCmd,
		Version:     Version1,
		PayloadSize: fileSize,
	}

	logrus.Infof("[ServiceVMImportLayer] Sending the tar file (%d bytes) stream to the LinuxServiceVM", fileSize)
	err = SendData(header, tmpFile, stdin)
	if err != nil {
		return 0, err
	}

	logrus.Infof("[ServiceVMImportLayer] waiting response from the LinuxServiceVM")
	payloadSize, err := waitForResponse(stdout)
	if err != nil {
		return 0, err
	}

	logrus.Infof("[ServiceVMImportLayer] reading back vhd stream (%d bytes) and write to VHD", payloadSize)
	// We are getting the VHD stream, so write it to file
	err = writeVHDFile(path.Join(layerPath, LayerVHDName), payloadSize, stdout)
	if err != nil {
		return 0, err
	}
	logrus.Infof("[ServiceVMImportLayer] new vhd file was created: [%s] ", path.Join(layerPath, LayerVHDName))
	return payloadSize, err
}

func ServiceVMExportLayer(vhdPath string) (io.ReadCloser, error) {
	logrus.Infof("[ServiceVMExportLayer] file %s\n", vhdPath)
	vhdFile, fileSize, err := getVHDFile(vhdPath)
	if err != nil {
		return nil, err
	}
	defer vhdFile.Close()

	// Execute tar_to_vhd as a external process in the ServiceVM for
	// converting a tar into a fixed VHD file
	process, err := launchProcessInServiceVM("./svm_utils")
	if err != nil {
		logrus.Errorf("launchProcessInServiceVM failed with %s", err)
		return nil, err
	}

	// get the std io pipes from the newly created process
	stdin, stdout, _, err := process.Stdio()
	if err != nil {
		logrus.Errorf("[ServiceVMExportLayer]  getting std pipes failed %s", err)
		return nil, err
	}

	header := &ServiceVMHeader{
		Command:     ExportCmd,
		Version:     0,
		PayloadSize: fileSize,
	}

	logrus.Infof("VHD PATH = ", vhdFile.Name())
	err = SendData(header, vhdFile, stdin)
	if err != nil {
		logrus.Errorf("[ServiceVMExportLayer]  getting std pipes failed %s", err)
		return nil, err
	}

	logrus.Infof("Waiting for server response")
	payloadSize, err := waitForResponse(stdout)
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		//defer sendClose(stdout)
		logrus.Infof("Copying result over hvsock")
		io.CopyN(writer, stdout, payloadSize)
	}()
	return reader, nil
}

//
// Create a sandbox file named LayerSandboxName under sandboxFolder on the host
// This is donee by copying a prebuilt-sandbox from the ServiceVM
//
func ServiceVMCreateSandbox(sandboxFolder string) error {
	sandboxPath := path.Join(sandboxFolder, LayerSandboxName)
	fmt.Printf("ServiceVMCreateSandbox: Creating sandbox path: %s\n", sandboxPath)

	// launch a process in the ServiceVM for handling the sandbox creation
	process, err := launchProcessInServiceVM("./svm_utils")
	if err != nil {
		logrus.Infof("launchProcessInServiceVM failed with %s", err)
		return err
	}

	// get the std io pipes from the newly created process
	stdin, stdout, _, err := process.Stdio()
	if err != nil {
		logrus.Errorf("[ServiceVMCreateSandbox] getting std pipes from the newly created process failed %s", err)
		return err
	}

	// Prepare payload data for CreateSandboxCmd command
	hdr := &ServiceVMHeader{
		Command:     CreateSandboxCmd,
		Version:     Version1,
		PayloadSize: SandboxInfoHeaderSize,
	}

	hdrSandboxInfo := &SandboxInfoHeader{
		MaxSandboxSizeInMB: 19264, // in MB, 16*1024MB = 16 GB
	}
	// Send ServiceVMHeader and SandboxInfoHeader to the Service VM
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.BigEndian, hdrSandboxInfo); err != nil {
		return err
	}

	logrus.Infof("[ServiceVMCreateSandbox] Writing (%d) bytes to the Service VM", buf.Bytes())
	_, err = stdin.Write(buf.Bytes())
	if err != nil {
		return err
	}

	// wait for ServiceVM to response
	logrus.Infof("[ServiceVMCreateSandbox] wait response from ServiceVM")
	resultSize, err := waitForResponse(stdout)
	if err != nil {
		return err
	}

	logrus.Infof("writing vhdx stream to file")
	// Get back the sandbox VHDx stream from the service VM and write it to file
	err = writeVHDFile(sandboxPath, resultSize, stdout)
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
	createProcessParms.WorkingDirectory = "/mnt/gcs/LinuxServiceVM/scratch/bin"

	// Configure the environment for the process
	pathValue := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/mnt/gcs/LinuxServiceVM/scratch/bin"
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

func exportSandbox(sandboxFolder string) (io.ReadCloser, error) {
	logrus.Infof("exportSandbox")
	// do we need to do anything?
	return nil, nil
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
	buf := make([]byte, ServiceVMHeaderSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return 0, err
	}

	hdr, err := DeserializeHeader(buf)
	if err != nil {
		return 0, err
	}

	if hdr.Command != ResponseOKCmd {
		logrus.Infof("[waitForResponse] hdr.Command = 0x%0x", hdr.Command)
		return 0, fmt.Errorf("Service VM failed")
	}
	return hdr.PayloadSize, nil
}

func writeVHDFile(path string, bytesToRead int64, r io.Reader) error {
	fmt.Println(path)

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	_, err = io.CopyN(f, r, bytesToRead)
	if err != nil {
		return err
	}

	return f.Close()
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
		"-Path",
		pathName,
		"-VMName",
		ServiceVMName,
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
		"-ControllerType",
		"SCSI",
		"-ControllerNumber",
		cn,
		"-ControllerLocation",
		cl,
		"-VMName",
		ServiceVMName).Run()
	return err
}

func serializeSCSI(header *ServiceVMHeader, scsiHeader *SCSICodeHeader) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, header); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.BigEndian, scsiHeader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func getVHDFile(vhdPath string) (*os.File, int64, error) {
	// The vhd could be either layer.vhd or sandbox.vhdx depending on
	// if we are a ro layer or r/w layer
	vhdFile, err := os.Open(path.Join(vhdPath, LayerVHDName))
	if err == nil {
		return nil, 0, err
	} else if err != nil && !os.IsNotExist(err) {
		return nil, 0, err
	}

	// Try the sandbox path
	vhdFile, err = os.Open(path.Join(vhdPath, LayerSandboxName))
	if err != nil {
		return nil, 0, err
	}

	fileInfo, err := vhdFile.Stat()
	if err != nil {
		return nil, 0, err
	}
	return vhdFile, fileInfo.Size(), nil
}
