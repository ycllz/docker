package servicevm

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/rneugeba/virtsock/pkg/hvsock"
    "github.com/Sirupsen/logrus"
)

const (
	ImportCmd = iota
	ExportCmd
	CreateSandboxCmd
	ExportSandboxCmd
	TerminateCmd
	ResponseOKCmd
	ResponseFailCmd
)

const (
	Version1 = iota
	Version2
)

const ServiceVMHeaderSize = 16

type ServiceVMHeader struct {
	Command     uint32
	Version     uint32
	PayloadSize int64
}

const SCSICodeHeaderSize = 8

type SCSICodeHeader struct {
	ControllerNumber   uint32
	ControllerLocation uint32
}

const SandboxInfoHeaderSize = 4

type SandboxInfoHeader struct {
	MaxSandboxSizeInMB   uint32
}

const ConnTimeOut = 300
const LayerVHDName = "layer.vhd"
const LayerSandboxName = "sandbox.vhdx"
const ServiceVMName = "LinuxServiceVM"

var ServiceVMId hvsock.GUID
var ServiceVMSocketId, _ = hvsock.GUIDFromString("E9447876-BA98-444F-8C14-6A2FFF773E87")

func SendData(hdr *ServiceVMHeader, payload io.Reader, dest io.Writer) error {
	hdrBytes, err := SerializeHeader(hdr)
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
    max_transfer_size = 4096
    total_bytes_transfered = 0
    bytes_to_transfer =0

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
        total_bytes_transfered += bytes_transfered
		bytes_left -= bytes_transfered
	}
    logrus.Infof("[SendData] total_bytes_transfered = %d bytes sent to the LinuxServiceVM successfully", total_bytes_transfered)
	return err
}

func ReadHeader(r io.Reader) (*ServiceVMHeader, error) {
	hdr := &ServiceVMHeader{}
	buf, err := SerializeHeader(hdr)
	if err != nil {
		return nil, err
	}

	_, err = io.ReadFull(r, buf)
	if err != nil {
        logrus.Errorf("[DeserializeHeader]io.ReadFull %s", err)
		return nil, err
	}

	return DeserializeHeader(buf)
}

func SerializeHeader(hdr *ServiceVMHeader) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
        logrus.Errorf("[DeserializeHeader]binary.Write failed with %s", err)
		return nil, err
	}
	return buf.Bytes(), nil
}

func DeserializeHeader(hdr []byte) (*ServiceVMHeader, error) {
	buf := bytes.NewBuffer(hdr)
	hdrPtr := &ServiceVMHeader{}
	if err := binary.Read(buf, binary.BigEndian, hdrPtr); err != nil {
        logrus.Errorf("[DeserializeHeader]binary.Read failed with %s", err)
		return nil, err
	}
	return hdrPtr, nil
}
