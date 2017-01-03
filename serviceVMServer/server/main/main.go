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
    "io/ioutil"
    "bytes"
    "vhd"
)

func handleImportV1(conn *net.TCPConn, cn byte, cl byte) error {
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
    size, _, err := tarlib.Unpack(conn, folderStr)
    if err != nil {
        fmt.Println("tar error: failed to unpack tar")
        // don't return now, still need to clean up + umount
        fmt.Println(err.Error())
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

func roundpow2(i uint64) uint64 {
    if i == 0 {
        return uint64(1)
    }

    if (i & (i - 1)) != 0 {
        var j uint64 = 1
        for j < i {
            j <<= 1
        }
        i = j
    }
    return i
}

func fixSize(size uint64, inode uint64) (uint64, uint64) {
    if inode == 0 || size == 0 {
        return 0, 0
    }

    // Lets just assume we need 20% more inodes 
    inode = uint64(float64(inode) * 1.2)

    // round inode to power of 2
    inode = roundpow2(inode)

    // Inodes add 256 bytes
    size += inode * 256

    // Right now, we should have sufficient inodes for the file system
    // for the rest of the file system overhead, assume 20% more.
    size = uint64(float64(size) * 1.2)

    // Now, we need this constraint I think: Inodes * 1024 <= 64k <= size 
    if size < inode * 1024 {
        size = inode * 1024
    }
    if size < 64 * 1024 {
        size = 64 * 1024
    }

    // Finally, align to 8k
    if size % 8192 != 0 {
        size += 8192 - size % 8192
    }
    return size, inode
}

func handleImportV2(conn *net.TCPConn) error {
    // First, create a temp folder to extract to
    srcFolder, err := ioutil.TempDir("", "src-files")
    if err != nil {
        fmt.Printf("Failed to create temp dir for extraction %s\n", err.Error())
    }
    defer os.RemoveAll(srcFolder)

    // Now, a create mount directory
    mntFolder, err := ioutil.TempDir("", "mnt")
    if err != nil {
        fmt.Printf("Failed to create temp dir for mounting: %s\n", err.Error())
    }
    defer os.RemoveAll(mntFolder)

    // Now need to create the fixed VHD
    vhdFile, err := ioutil.TempFile("", "vhd")
    if err != nil {
        fmt.Printf("error creating vhd raw file: %s\n", err.Error())
    }
    defer os.Remove(vhdFile.Name())
    vhdFileName := vhdFile.Name()
    vhdFile.Close()

    // Now, we unpack the tar file.
    size, num, err := tarlib.Unpack(conn, srcFolder)
    if err != nil {
        fmt.Printf("tar error: failed to unpack tar. %s\n", err.Error())
        // don't return now, still need to clean up + umount
    }

    // Adjust the size to account for file system overhead
    fmt.Printf("Fixing size, num: %d, %d\n", size, num)
    new_size, new_num := fixSize(size, num)

    // Copy data to vhd file.
    fmt.Printf("Copying %s -> %s (dev %s)\n", srcFolder, mntFolder, vhdFile.Name())
    ssize, snum := strconv.Itoa(int(new_size)), strconv.Itoa(int(new_num))
    err = exec.Command("./create_fixed_vhd.sh", ssize, snum, vhdFile.Name(), mntFolder, srcFolder).Run()
    if err != nil {
        fmt.Printf("error in create vhd script: %s\n", err.Error())
    }

    // Now send back to the client
    fmt.Printf("Sending response with size: %d\n", size)
    hdr := [4]byte{winlx.ResponseOKCmd, 0, 0, 0}
    buf := [8]byte{}
    binary.BigEndian.PutUint64(buf[:], size)

    packet := append(hdr[:], buf[:]...)
    fmt.Println(packet, len(packet))

    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.ConnTimeOut)))
    _, err = conn.Write(packet)
    if err != nil {
        fmt.Printf("error in sending header packet: %s\n", err.Error())
        return err
    }

    // Send VHD file
    fmt.Println("Opending VHD file")
    vhdFile2, err := os.Open(vhdFileName)
    if err != nil {
        fmt.Printf("error in opening vhd file\n")
    }
    defer vhdFile2.Close()

    fmt.Println("Sending VHD File")
    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.WaitTimeOut)))
    _, err = io.Copy(conn, vhdFile2)
    if err != nil {
        fmt.Printf("error in transfering vhd back %s\n", err.Error())
        return err
    }

    // Send VHD footer
    fmt.Println("Creating VHD footer")
    vhdr, err := vhd.NewVHDHeaderFixed(new_size)
    if err != nil {
        fmt.Printf("error in creating fixed vhd header: %s\n", err.Error())
        return err
    }

    vhdrb := &bytes.Buffer{}
    err = binary.Write(vhdrb, binary.BigEndian, vhdr)
    if err != nil {
        fmt.Printf("error in serializing vhd header %s\n", err.Error())
        return err
    }

    fmt.Println("sending vhd footer")
    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.ConnTimeOut)))
    _, err = conn.Write(vhdrb.Bytes())
    if err != nil {
        fmt.Printf("error in sending VHD footer %s\n", err.Error())
        return err
    }

    conn.WriteClose()
    return nil
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
        if hdr[3] == winlx.Version1 {
            return handleImportV1(conn, hdr[1], hdr[2])
        } else {
            return handleImportV2(conn)
        }
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
