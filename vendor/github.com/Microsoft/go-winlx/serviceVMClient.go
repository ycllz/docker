package winlx

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
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

func attachLayerVHD(layerPath string, serviceVMName string) (uint8, error) {
	// TODO: Change this into go code / some dll.
	out, err := exec.Command("powershell", "C:\\gopath\\bin\\ATTACH_VHD.ps1", serviceVMName, layerPath).Output()
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

func sendImportLayer(c *net.TCPConn, hdr []byte, r io.Reader) error {
	// First send the header, then the payload, then EOF
	c.SetWriteDeadline(time.Now().Add(time.Duration(ConnTimeOut * time.Second)))
	_, err := c.Write(hdr)
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

	if buf[0] != ResponseOKCmd {
		return 0, fmt.Errorf("Service VM failed")
	}
	return int64(binary.BigEndian.Uint64(buf[4:])), nil
}

func detachedLayerVHD(id uint8, serviceVMName string) error {
	cNum, cLoc := UnpackLUN(id)
	cns, cls := strconv.Itoa(int(cNum)), strconv.Itoa(int(cLoc))

	fmt.Println(cns, cls)
	return exec.Command(
		"powershell",
		"Remove-VMHardDiskDrive",
		"-VMName",
		serviceVMName,
		"-ControllerType",
		"SCSI",
		"-ControllerNumber",
		cns,
		"-ControllerLocation",
		cls).Run()
}

func writeVHDFile(path string, r io.Reader) error {
	fmt.Println(path)
	f, err := os.Create(path)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	_, err = io.Copy(f, r)
	f.Close()
	return err
}

func ServiceVMImportLayer(layerPath string, reader io.Reader, scfg ServiceVMConfig) (int64, error) {
	var id uint8
	var err error

	version := scfg.Version
	if version == Version1 {
		id, err = attachLayerVHD(path.Join(layerPath, LayerVHDName), scfg.Name)
		if err != nil {
			return 0, err
		}
	}

	conn, err := connectToServer(scfg.Address)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	header := CreateHeader(ImportCmd, id, version)

	buf := []byte{header.Command, header.SCSIControllerNum, header.SCSIDiskNum, version}

	err = sendImportLayer(conn, buf, reader)
	if err != nil {
		return 0, err
	}

	size, err := waitForResponse(conn)
	if err != nil {
		return 0, err
	}

	if version == Version1 {
		// We are done so detach the VHD
		err = detachedLayerVHD(id, scfg.Name)
	} else {
		// We are getting the VHD stream, so write it to file
		fmt.Println("writing vhd stream")
		err = writeVHDFile(path.Join(layerPath, LayerVHDName), conn)
	}

	if err != nil {
		size = 0
	}
	return size, err
}
