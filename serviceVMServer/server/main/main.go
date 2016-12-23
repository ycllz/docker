package main

import (
    "io"
    "net"
    "sync"
    "fmt"
    "strconv"
    "time"
    "os/exec"
    "encoding/binary"
    "os"
    "winlx"
    "tarlib"
)

func handleImport(conn *net.TCPConn, cn byte, cl byte) error {
    // Set up the mount
    cnS, clS := strconv.Itoa(int(cn)), strconv.Itoa(int(cl))
    folderStr := "/mnt-" + cnS + "-" + clS

    err := exec.Command("./createlayer.sh", cnS, clS, folderStr).Run()
    if err != nil {
        fmt.Println("os error: failed to create layer")
        return err
    }

    // Write the tar file to it.
    fmt.Printf("Writing to folder: %s\n", folderStr)
    size, err := tarlib.Unpack(conn, folderStr)
    if err != nil {
        fmt.Println("tar error: failed to unpack tar")
        // don't return now, still need to clean up + umount
    }

    // Unmount
    fmt.Println("Cleaning up mounts")
    err = exec.Command("umount", folderStr).Run();
    if err != nil {
        fmt.Println("os error: failed to unmount layer")
        return err
    }

    err = os.RemoveAll(folderStr)
    if err != nil {
        fmt.Println("os error: failed to remove mounted folder")
        return err
    }


    // Send the return
    // TODO: Actually handle the failure case properly
    fmt.Printf("Sending response with size: %d\n", size)
    hdr := [4]byte{winlx.ResponseOKCmd, 0, 0, 0}
    buf := [8]byte{}
    binary.BigEndian.PutUint64(buf[:], size)

    packet := append(hdr[:], buf[:]...)
    fmt.Println(packet, len(packet))

    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.ConnTimeOut)))
    _, err = conn.Write(packet)
    return err
}

func handleSingleClient(conn *net.TCPConn) error {
    hdr := [4]byte{}

    conn.SetReadDeadline(time.Now().Add(time.Duration(time.Second * winlx.ConnTimeOut)))
    _, err := io.ReadFull(conn, hdr[:])
    if err != nil {
        fmt.Println("timeout: closing connection with client")
        return err
    }

    if hdr[0] == winlx.ImportCmd {
        return handleImport(conn, hdr[1], hdr[2])
    }

    // TODO: Handle the export later. 
    return nil
}

func ServiceVMAcceptClients() {
    addr, err := net.ResolveTCPAddr("tcp", winlx.ServiceVMAddress)
    if err != nil {
        return
    }

    listener, err := net.ListenTCP("tcp", addr)
    if err != nil {
        return
    }

    var wg sync.WaitGroup
    for {
        conn, err := listener.AcceptTCP()
        if err != nil {
            break
        }

        fmt.Println("got a new client.")
        wg.Add(1)
        go func() {
            handleSingleClient(conn)
            fmt.Println("done with client")
            conn.Close()
            wg.Done()
        }()
    }

    // For all current connections wait until they are finished
    listener.Close()
    wg.Wait()
}

func main() {
    fmt.Printf("waiting for clients on %s...\n", winlx.ServiceVMAddress)
    ServiceVMAcceptClients()
}
