package winlx

import (
	"encoding/binary"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"

	"strings"

	"io/ioutil"

	"github.com/Microsoft/hvsock"
	"strconv"
)

func init() {
	// Get ID for hvsock. For now, assume that it is always up.
	// So, ignore the err for now.
	cmd := fmt.Sprintf("$(Get-ComputeProcess %s).Id", ServiceVMName)
	result, _ := exec.Command("powershell", cmd).Output()
	ServiceVMId, _ = hvsock.GUIDFromString(strings.TrimSpace(string(result)))
}

func ServiceVMImportLayer(layerPath string, reader io.Reader) (int64, error) {
	// Hv sockets don't support graceful/unidirectional shutdown, and the
	// hvsock wrapper works weirdly with the tar reader, so we first write the
	// contents to a temp file.
	tmpFile, fileSize, err := storeReader(reader)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	conn, err := connectToServer()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	header := &ServiceVMHeader{
		Command:     ImportCmd,
		Version:     Version1,
		PayloadSize: fileSize,
	}

	err = SendData(header, tmpFile, conn)
	if err != nil {
		return 0, err
	}

	resultSize, err := waitForResponse(conn)
	if err != nil {
		return 0, err
	}

	fmt.Println("writing vhd to file.")
	// We are getting the VHD stream, so write it to file
	err = writeVHDFile(path.Join(layerPath, LayerVHDName), resultSize, conn)
	if err != nil {
		return 0, err
	}

	err = sendClose(conn)
	if err != nil {
		return 0, err
	}

	return resultSize, err
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

func connectToServer() (hvsock.Conn, error) {
	hvAddr := hvsock.HypervAddr{VMID: ServiceVMId, ServiceID: ServiceVMSocketId}
	return hvsock.Dial(hvAddr)
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

func sendClose(rc io.WriteCloser) error {
	header := &ServiceVMHeader{
		Command:     TerminateCmd,
		Version:     Version1,
		PayloadSize: 0,
	}

	buf, err := SerializeHeader(header)
	if err != nil {
		return err
	}

	_, err = rc.Write(buf)
	if err != nil {
		return err
	}
	return rc.Close()
}

func ServiceVMExportLayer(vhdPath string) (io.ReadCloser, error) {
	vhdFile, fileSize, err := getVHDFile(vhdPath)
	if err != nil {
		return nil, err
	}
	defer vhdFile.Close()

	conn, err := connectToServer()
	if err != nil {
		return nil, err
	}

	header := &ServiceVMHeader{
		Command:     ExportCmd,
		Version:     0,
		PayloadSize: fileSize,
	}

	fmt.Println("VHD PATH = ", vhdFile.Name())
	err = SendData(header, vhdFile, conn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	fmt.Println("Waiting for server response")
	payloadSize, err := waitForResponse(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		defer sendClose(conn)
		fmt.Println("Copying result over hvsock")
		io.CopyN(writer, conn, payloadSize)
	}()
	return reader, nil
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

func ServiceVMCreateSandbox(sandboxFolder string) error {
	sandboxPath := path.Join(sandboxFolder, LayerSandboxName)
	fmt.Printf("ServiceVMCreateSandbox: Creating sandbox path: %s\n", sandboxPath)

	err := newVHDX(sandboxPath)
	if err != nil {
		return err
	}

	controllerNumber, controllerLocation, err := attachVHDX(sandboxPath)
	if err != nil {
		return err
	}
	fmt.Printf("ServiceVMCreateSandbox: Got Controller number: %d controllerLocation: %d\n", controllerNumber, controllerLocation)

	hdr := &ServiceVMHeader{
		Command: CreateSandboxCmd,
		Version: Version1,
		PayloadSize: SCSICodeHeaderSize,
	}

	scsiHeader := &SCSICodeHeader{
		ControllerNumber: controllerNumber,
		ControllerLocation: controllerLocation,
	}
	return sendSCSINumbers(hdr, scsiHeader)
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

func sendSCSINumbers(header *ServiceVMHeader, scsiHeader *SCSICodeHeader) error {
	conn, err := connectToServer()
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, header); err != nil {
		return err
	}

	if err := binary.Write(buf, binary.BigEndian, scsiHeader); err != nil {
		return err
	}

	_, err = conn.Write(buf.Bytes())
	if err != nil {
		return err
	}

	_, err = waitForResponse(conn)
	if err != nil {
		return err
	}

	err = sendClose(conn)
	if err != nil {
		return err
	}
	return nil
}