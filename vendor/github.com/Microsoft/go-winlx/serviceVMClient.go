package winlx

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"

	"strings"

	"github.com/Microsoft/hvsock"
)

func init() {
	// Get ID for hvsock. For now, assume that it is always up.
	// So, ignore the err for now.
	cmd := fmt.Sprintf("$(Get-ComputeProcess %s).Id", ServiceVMName)
	result, _ := exec.Command("powershell", cmd).Output()
	ServiceVMId, _ = hvsock.GUIDFromString(strings.TrimSpace(string(result)))
}

func connectToServer() (net.Conn, error) {
	hvAddr := hvsock.HypervAddr{VMID: ServiceVMId, ServiceID: ServiceVMSocketId}
	return hvsock.Dial(hvAddr)
}

func sendImportLayer(c net.Conn, hdr []byte, r io.Reader) error {
	// First send the header, then the payload, then EOF
	_, err := c.Write(hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(c, r)

	// I originally expected that c.CloseWrite() is required to send EOF, but
	// it looks like io.Copy sends EOF without sending the shutdown write control code.
	return err
}

func waitForResponse(c net.Conn) (int64, error) {
	// Read header
	// TODO: Handle error case
	buf := [12]byte{}
	_, err := io.ReadFull(c, buf[:])
	if err != nil {
		return 0, err
	}

	if buf[0] != ResponseOKCmd {
		return 0, fmt.Errorf("Service VM failed")
	}
	return int64(binary.BigEndian.Uint64(buf[4:])), nil
}

func writeVHDFile(path string, r io.Reader) error {
	fmt.Println(path)
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, r)
	if err != nil {
		// If the server closes the connection, then io.Copy might return an
		// error since the connection is already closed. This is okay because
		// all of the data has been read, so we are ready to close anyway.
		if err == hvsock.ErrSocketClosed || err == hvsock.ErrSocketReadClosed {
			err = nil
		}
	}
	f.Close()
	return err
}

func ServiceVMImportLayer(layerPath string, reader io.Reader) (int64, error) {
	conn, err := connectToServer()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	header := ServiceVMHeader{
		Command:           ImportCmd,
		Version:           Version2,
		SCSIControllerNum: 0,
		SCSIDiskNum:       0,
	}

	buf, err := SerializeHeader(&header)
	if err != nil {
		return 0, err
	}

	err = sendImportLayer(conn, buf, reader)
	if err != nil {
		return 0, err
	}

	size, err := waitForResponse(conn)
	if err != nil {
		return 0, err
	}

	// We are getting the VHD stream, so write it to file
	err = writeVHDFile(path.Join(layerPath, LayerVHDName), conn)
	if err != nil {
		size = 0
	}
	return size, err
}
