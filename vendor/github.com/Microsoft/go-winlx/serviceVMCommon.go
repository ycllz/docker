package winlx

import (
	"bytes"
	"encoding/binary"

	"github.com/Microsoft/hvsock"
)

// The protocol between the service VM and docker is very simple:
// All numbers are in network order.
// Import Layer:
//      - Docker sends ImportCmd + tar stream.
//      - After sending all of the stream, it does a TCP Close Write to send EOF.
//      - The service VM reads + extracts until EOF.
//          - It sends a response header + data: on success, it sends a ResponseOK and the 8 byte size
//            written; on failure, it sets ResponseFailCmd
//
// Export Layer:
//      - Docker sends a ExportCmd signfying that it wants to tar the files.
//      - Service VM tars and returns the tar stream. Does TCP write close to indicate eof.
//          - On success sends ResponseOK, on failure sends ResponseFail
//      - Docker reads until eof and continues with rest functions.

const (
	ImportCmd = iota
	ExportCmd
	ResponseOKCmd
	ResponseFailCmd
)

const (
	Version1 = iota
	Version2
)

type ServiceVMHeader struct {
	Command           uint8
	Version           uint8
	SCSIControllerNum uint8 // Used for Version 1, but not used anymore.
	SCSIDiskNum       uint8 // Used for Version 1, but not used anymore.
}

const ConnTimeOut = 300
const LayerVHDName = "layer.vhd"
const ServiceVMName = "ZUbuntu1604-Dev"

var ServiceVMId hvsock.GUID
var ServiceVMSocketId, _ = hvsock.GUIDFromString("E9447876-BA98-444F-8C14-6A2FFF773E87")

func SerializeHeader(hdr *ServiceVMHeader) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DeserializeHeader(hdr []byte) (*ServiceVMHeader, error) {
	buf := bytes.NewBuffer(hdr)
	hdrPtr := &ServiceVMHeader{}
	if err := binary.Read(buf, binary.BigEndian, hdrPtr); err != nil {
		return nil, err
	}
	return hdrPtr, nil
}
