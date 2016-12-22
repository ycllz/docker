package main

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

type ServiceVMHeader struct {
    Command           uint8
    SCSIControllerNum uint8
    SCSIDiskNum       uint8
    Reserved          uint8
}

const connTimeOut = 10
const waitTimeOut = 120
const serviceVMName = "Ubuntu1604-v4"
const serviceVMAddress = "10.123.175.141:5931"
const layerVHDName = "layer.vhd"

func unpackLUN(lun uint8) (uint8, uint8) {
    return (lun >> 6), lun & 0x3F
}

func packLUN(cNum, dNum uint8) uint8 {
    return (cNum << 6) | (dNum & 0x3F)
}

func createHeader(c uint8, lun uint8) ServiceVMHeader {
    cNum, dNum := unpackLUN(lun)
    return ServiceVMHeader{
        Command:           c,
        SCSIControllerNum: cNum,
        SCSIDiskNum:       dNum,
        // Go automatically sets Reserved to 0
    }
}

