package winlx

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

type tcpDialResult struct {
	conn *net.TCPConn
	err  error
}

func attachLayerVHD(layerPath string) (uint8, error) {
	// TODO: Change this into go code / some dll.
	out, err := exec.Command("powershell", "C:\\gopath\\bin\\ATTACH_VHD.ps1", ServiceVMName, layerPath).Output()
	if err != nil {
		return 0, err
	}

	s := strings.TrimSpace(string(out))
	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return 0, err
	}
	return uint8(n), err
}

func findServerIP() string {
	// TODO: Find this more programmatically. assume its hardcoded for now.
	return ServiceVMAddress
}

func connectToServer(ip string) (*net.TCPConn, error) {
	addr, err := net.ResolveTCPAddr("tcp", ip)
	if err != nil {
		return nil, err
	}

	// No support for DialTimeout for TCP, so need to manually do this
	c := make(chan tcpDialResult)

	go func() {
		conn, err := net.DialTCP("tcp", nil, addr)
		c <- tcpDialResult{conn, err}
	}()

	select {
	case res := <-c:
		return res.conn, res.err
	case <-time.After(time.Second * ConnTimeOut):
		return nil, fmt.Errorf("timeout on dialTCP")
	}
}

func sendImportLayer(c *net.TCPConn, id uint8, r io.Reader) error {
	header := CreateHeader(ImportCmd, id)
	buf := []byte{header.Command, header.SCSIControllerNum, header.SCSIDiskNum, 0}

	// First send the header, then the payload, then EOF
	c.SetWriteDeadline(time.Now().Add(time.Duration(ConnTimeOut * time.Second)))
	_, err := c.Write(buf)
	if err != nil {
		return err
	}

	c.SetWriteDeadline(time.Now().Add(time.Duration(WaitTimeOut * time.Second)))
	_, err = io.Copy(c, r)
	if err != nil {
		return err
	}

	c.SetWriteDeadline(time.Now().Add(time.Duration(ConnTimeOut * time.Second)))
	err = c.CloseWrite()
	if err != nil {
		return err
	}
	return nil
}

func waitForResponse(c *net.TCPConn) (int64, error) {
	c.SetReadDeadline(time.Now().Add(time.Duration(WaitTimeOut * time.Second)))

	// Read header
	// TODO: Handle error case
	buf := [12]byte{}
	_, err := io.ReadFull(c, buf[:])
	if err != nil {
		return 0, err
	}
	fmt.Println("got server response")

	if buf[0] != ResponseOKCmd {
		fmt.Println("service VM failed")
		return 0, fmt.Errorf("Service VM failed")
	}
	return int64(binary.BigEndian.Uint64(buf[4:])), nil
}

func detachedLayerVHD(layerPath string, id uint8) error {
	cNum, cLoc := UnpackLUN(id)

	return exec.Command(
		"powershell",
		"Remove-VMHardDiskDrive",
		"-VMName",
		LayerVHDName,
		"-ControllerType",
		"SCSI",
		"-ControllerNumber",
		string(cNum),
		"-ControllerLocation",
		string(cLoc),
	).Run()
}

func ServiceVMImportLayer(layerPath string, reader io.Reader) (int64, error) {
	id, err := attachLayerVHD(path.Join(layerPath, LayerVHDName))
	if err != nil {
		return 0, err
	}

	conn, err := connectToServer(findServerIP())
	defer conn.Close()
	if err != nil {
		return 0, err
	}

	err = sendImportLayer(conn, id, reader)
	if err != nil {
		return 0, err
	}

	size, err := waitForResponse(conn)
	if err != nil {
		return 0, err
	}

	err = detachedLayerVHD(path.Join(layerPath, LayerVHDName), id)
	if err != nil {
		return 0, err
	}
	return size, err
}
