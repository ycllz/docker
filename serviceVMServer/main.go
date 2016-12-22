package main

import (
    "io"
    "net"
    "sync"
    "fmt"
    "strconv"
    "time"
    "os/exec"
)

const serviceVMAddress = "10.123.175.141:5931"
const serviceVMServerTimeout = 10

func handleImport(conn *net.TCPConn, cn byte, cl byte) {
    cnS, clS := strconv.Itoa(int(cn)), strconv.Itoa(int(cl))
    folder, err := exec.Command("createlayer.sh", cnS, clS).Output()
    if err != nil {
        fmt.Println("os error: failed to create layer")
    }

    folderStr := string(folder)
    fmt.Println("Writing to folder: %s\n", folderStr)
    Unpack(conn, folderStr)
}

func handleSingleClient(conn *net.TCPConn) {
    hdr := [4]byte{}

    conn.SetReadDeadline(time.Now().Add(time.Duration(time.Second * serviceVMServerTimeout)))
    _, err := io.ReadFull(conn, hdr[:])
    if err != nil {
        fmt.Println("timeout: closing connection with client")
        return
    }

    if hdr[0] == 0 {
        handleImport(conn, hdr[1], hdr[2])
    }
}

func ServiceVMAcceptClients() {
    addr, err := net.ResolveTCPAddr("tcp", serviceVMAddress)
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
            conn.Close()
            wg.Done()
        }()
    }

    // For all current connections wait until they are finished
    listener.Close()
    wg.Wait()
}

func main() {
    fmt.Printf("waiting for clients on %s...\n", serviceVMAddress)
    ServiceVMAcceptClients()
}
