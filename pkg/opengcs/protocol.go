package opengcs

import (
	"fmt"
	"io"

	"github.com/Sirupsen/logrus"
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

	serviceVMHeaderSize   = 16
	scsiCodeHeaderSize    = 8
	sandboxInfoHeaderSize = 4
	serviceVMName         = "LinuxServiceVM"
	socketID              = "E9447876-BA98-444F-8C14-6A2FFF773E87"
)

type scsiCodeHeader struct {
	controllerNumber   uint32
	controllerLocation uint32
}
type sandboxInfoHeader struct {
	maxSandboxSizeInMB uint32
}

type protocolCommandHeader struct {
	Command     uint32
	Version     uint32
	PayloadSize int64
}

func waitForResponse(r io.Reader) (int64, error) {
	buf := make([]byte, serviceVMHeaderSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return 0, err
	}

	hdr, err := deserializeHeader(buf)
	if err != nil {
		return 0, err
	}

	if hdr.Command != cmdResponseOK {
		logrus.Debugf("[waitForResponse] hdr.Command = 0x%0x", hdr.Command)
		return 0, fmt.Errorf("Service VM failed")
	}
	return hdr.PayloadSize, nil
}

func sendData(hdr *protocolCommandHeader, payload io.Reader, dest io.Writer) error {
	hdrBytes, err := serializeHeader(hdr)
	if err != nil {
		return err
	}
	logrus.Debugf("[SendData] Total bytes to send %d", hdr.PayloadSize)

	_, err = dest.Write(hdrBytes)
	if err != nil {
		return err
	}

	// break into 4Kb chunks
	var (
		maxTransferSize       int64 = 4096
		bytesToTransfer       int64
		totalBytesTransferred int64
	)

	bytesLeft := hdr.PayloadSize

	for bytesLeft > 0 {
		if bytesLeft >= maxTransferSize {
			bytesToTransfer = maxTransferSize
		} else {
			bytesToTransfer = bytesLeft
		}

		bytesTransferred, err := io.CopyN(dest, payload, bytesToTransfer)
		if err != nil && err != io.EOF {
			logrus.Errorf("[SendData] io.Copy failed with %s", err)
			return err
		}
		totalBytesTransferred += bytesTransferred
		bytesLeft -= bytesTransferred
	}
	logrus.Debugf("[SendData] totalBytesTransferred = %d bytes sent to the LinuxServiceVM successfully", totalBytesTransferred)
	return err
}
