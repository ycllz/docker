package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
	"os"
)

type tcpDialResult struct {
	conn *net.TCPConn
	err  error
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
	case <-time.After(time.Second * connTimeOut):
		return nil, fmt.Errorf("timeout on dialTCP")
	}
}

func sendImportLayer(c *net.TCPConn, id uint8, r io.Reader) error {
	header := createHeader(ImportCmd, id)
	buf := []byte{header.Command, header.SCSIControllerNum, header.SCSIDiskNum, 0}

	// First send the header, then the payload, then EOF
	c.SetWriteDeadline(time.Now().Add(time.Duration(connTimeOut * time.Second)))
	_, err := c.Write(buf)
	if err != nil {
		return err
	}

	c.SetWriteDeadline(time.Now().Add(time.Duration(waitTimeOut * time.Second)))
	_, err = io.Copy(c, r)
	if err != nil {
		return err
	}

	c.SetWriteDeadline(time.Now().Add(time.Duration(connTimeOut * time.Second)))
	err = c.CloseWrite()
	if err != nil {
		return err
	}
	return nil
}

func waitForResponse(c *net.TCPConn) (int64, error) {
	c.SetReadDeadline(time.Now().Add(time.Duration(waitTimeOut * time.Second)))

	// Read header
	hdr := [4]byte{}
	_, err := io.ReadFull(c, hdr[:])
	if err != nil {
		return 0, err
	}

	if hdr[0] != ResponseOKCmd {
		return 0, fmt.Errorf("Service VM failed")
	}

	// If service VM succeeded, read the size
	size := [8]byte{}
	c.SetReadDeadline(time.Now().Add(time.Duration(waitTimeOut * time.Second)))
	_, err = io.ReadFull(c, size[:])
	if err != nil {
		return 0, err
	}

	return int64(binary.BigEndian.Uint64(size[:])), nil
}

func ServiceVMImportLayer(reader io.Reader, id byte) (int64, error) {
	conn, err := connectToServer(serviceVMAddress)
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

	return size, err
}

func main() {
	if len(os.Args) != 4 {
		fmt.Printf("Usage: %s <tarpath> <scsi-num> <scsi-disk-num>\n")
		return
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Printf("Failed to open tar file\n")
		return
	}
	defer file.Close()

	n, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Printf("Failed to convert scsi num\n")
		return
	}

	l, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Println("Failed to convert scsi location")
		return
	}

	if n >= 4 || l >= 64 {
		fmt.Println("Incorrect size of scsi numbers")
		return
	}

	id := byte((n << 6) | l)
	size, err := ServiceVMImportLayer(file, id)
	if err != nil {
		fmt.Println("Failed to import layer")
		return
	}

	fmt.Printf("Success! Got size = %d\n", size)
}

