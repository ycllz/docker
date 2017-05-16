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

	serviceVMHeaderSize = 16
	scsiCodeHeaderSize  = 8
	connTimeOut         = 300
	layerVHDName        = "layer.vhd"
	layerSandboxName    = "sandbox.vhdx"
	serviceVMName       = "LinuxServiceVM"
	socketID            = "E9447876-BA98-444F-8C14-6A2FFF773E87"
)

type serviceVMHeader struct {
	command     uint32
	version     uint32
	payloadSize int64
}

type scsiCodeHeader struct {
	controllerNumber   uint32
	controllerLocation uint32
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
}

// importLayer inports a layer to a service VM
func importLayer(layerPath string, reader io.Reader) (int64, error) {
	// Hv sockets don't support graceful/unidirectional shutdown, and the
	// hvsock wrapper works weirdly with the tar reader, so we first write the
	// contents to a temp file.
	logrus.Debugf("importLayer path %s", layerPath)
	tmpFile, fileSize, err := storeReader(reader)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	logrus.Debugf("importLayer connecting")
	conn, err := connect()
	if err != nil {
		return 0, err
	}
	defer closeConnection(conn)

	header := &serviceVMHeader{
		command:     cmdImport,
		version:     version1,
		payloadSize: fileSize,
	}

	err = sendData(header, tmpFile, conn)
	if err != nil {
		return 0, err
	}

	resultSize, err := waitForResponse(conn)
	if err != nil {
		return 0, err
	}

	// We are getting the VHD stream, so write it to file
	logrus.Debugf("importLayer path %s, writing to %s", layerPath, layerVHDName)
	err = writeVHDFile(path.Join(layerPath, layerVHDName), resultSize, conn)
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

func connect() (hvsock.Conn, error) {
	hvAddr := hvsock.HypervAddr{VMID: serviceVMId, ServiceID: serviceVMSocketID}
	return hvsock.Dial(hvAddr)
}

func waitForResponse(r io.Reader) (int64, error) {
	// Does this need a timeout?
	buf := make([]byte, serviceVMHeaderSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return 0, err
	}

	hdr, err := deserializeHeader(buf)
	if err != nil {
		return 0, err
	}

	if hdr.command != cmdResponseOK {
		return 0, fmt.Errorf("Service VM failed")
	}
	return hdr.payloadSize, nil
}

func writeVHDFile(path string, bytesToRead int64, r io.Reader) error {
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

func closeConnection(rc io.WriteCloser) error {
	logrus.Debugf("closing connection to service VM")
	header := &serviceVMHeader{
		command:     cmdTerminate,
		version:     version1,
		payloadSize: 0,
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

	fileInfo, err := vhdFile.Stat()
	if err != nil {
		return nil, err
	}

	conn, err := connect()
	if err != nil {
		return nil, err
	}

	header := &serviceVMHeader{
		command:     cmdExport,
		version:     0,
		payloadSize: fileInfo.Size(),
	}

	err = sendData(header, vhdFile, conn)
	if err != nil {
		closeConnection(conn)
		return nil, err
	}

	payloadSize, err := waitForResponse(conn)
	if err != nil {
		closeConnection(conn)
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
		io.CopyN(writer, conn, payloadSize)
		closeConnection(conn)
		writer.Close()
	}()
	return reader, nil
}

// createSandbox creates a r/w sandbox layer
func createSandbox(sandboxFolder string) error {
	sandboxPath := path.Join(sandboxFolder, layerSandboxName)
	logrus.Debugf("createSandbox path: %s\n", sandboxPath)

	err := newVHDX(sandboxPath)
	if err != nil {
		return err
	}

	controllerNumber, controllerLocation, err := attachVHDX(sandboxPath)
	if err != nil {
		return err
	}
	defer detachVHDX(controllerNumber, controllerLocation)
	logrus.Debugf("createSandbox: controllerNumber: %d controllerLocation: %d", controllerNumber, controllerLocation)

	hdr := &serviceVMHeader{
		command:     cmdCreateSandbox,
		version:     version1,
		payloadSize: scsiCodeHeaderSize,
	}

	scsiHeader := &scsiCodeHeader{
		controllerNumber:   controllerNumber,
		controllerLocation: controllerLocation,
	}

	conn, err := connect()
	if err != nil {
		return err
	}
	defer closeConnection(conn)

	data, err := serializeSCSI(hdr, scsiHeader)
	if err != nil {
		return err
	}

	_, err = conn.Write(data)
	if err != nil {
		return err
	}

	_, err = waitForResponse(conn)
	return err
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
	logrus.Debugf("exportSandbox creating sandbox at %s", sandboxPath)

	controllerNumber, controllerLocation, err := attachVHDX(sandboxPath)
	if err != nil {
		return nil, err
	}
	defer detachVHDX(controllerNumber, controllerLocation)
	logrus.Debugf("exportSandbox controllerNumber %d controllerLocation %d", controllerNumber, controllerLocation)

	hdr := &serviceVMHeader{
		command:     cmdExportSandbox,
		version:     version1,
		payloadSize: scsiCodeHeaderSize,
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

	if _, err = conn.Write(data); err != nil {
		return nil, err
	}

	payloadSize, err := waitForResponse(conn)
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
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

	_, err = dest.Write(hdrBytes)
	if err != nil {
		return err
	}

	_, err = io.CopyN(dest, payload, hdr.payloadSize)
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
