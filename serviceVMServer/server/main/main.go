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
    "strings"
    "archive/tar"
)

func parseConfig(f string) (string, error) {
    buf, err := ioutil.ReadFile(f)
    if err != nil {
        return "", err
    }

    strBuf := string(buf)
    fields := strings.Split(strBuf, "\n")

    for i := 0; i < len(fields); i++ {
        if strings.HasPrefix(fields[i], "IP=") {
            return fields[i][len("IP="):], nil
        }
    }
    return "", fmt.Errorf("no ip found")
}

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

func split_script_output(buf []byte) (uint64, uint64, error) {
    str := strings.Split(string(buf), "\n")
    if len(str) != 2 {
        return 0, 0, fmt.Errorf("invalid create vhd script output\n")
    }

    size, err := strconv.ParseUint(str[0], 10, 64)
    if err != nil {
        return 0, 0, err
    }

    new_size, err := strconv.ParseUint(str[1], 10, 64)
    if err != nil {
        return 0, 0, err
    }
    return size, new_size, nil
}

func alignN(n uint64, alignto uint64) uint64 {
    if n % alignto == 0 {
        return n
    }
    return n + alignto - n % alignto
}

func getBufAndEstimateSize(r io.Reader) (io.Reader, uint64, uint64, error) {
    rout := &bytes.Buffer{}
    tw := tar.NewWriter(rout)

    tr := tar.NewReader(r)
    size := uint64(2048)       // boot sector + super block 
    numInode := uint64(11)  // ext4 has 11 reserved inodes 
    inodeSize := uint64(256)
    blockSize := uint64(1024)

    // Now estimate the size based off the tar contents
    // Some assumptions:
    //  - Ext4 file system
    //  - Block size is 1024 bytes 
    //  - Inodes are 128 bytes
    //  - Assume that a new directory entry takes blocksize.
    //  - No journal, GDT table
    var err error
    for {
        hdr, err := tr.Next()
        if err == io.EOF {
            err = nil
            break
        }
        if err != nil {
            goto AfterLoop
        }

        numInode++
        switch hdr.Typeflag {
        case tar.TypeDir:
            // 1 directory entry for parent.
            // 1 inode with 2 directory entries ("." & ".." as data
            size += inodeSize + 2 * blockSize
        case tar.TypeReg, tar.TypeRegA:
            // 1 directory entry
            // 1 inode
            size += inodeSize + blockSize

            // The actual blocks used depends on the implementation
            // Each extent can hold 32k blocks, so 32M of data, so 128MB can get held
            // in the 4 extends below the i_block. Let's assume that every file is < 128MB, so
            // to avoid a deeper extent tree. So, we can just align to 1k
            size += alignN(uint64(hdr.Size), blockSize)
        case tar.TypeLink:
            // Hard Links share the same inode but ad a new dir entry.
            numInode--
            size += blockSize
        case tar.TypeSymlink:
            size += inodeSize
            if len(hdr.Name) > 60 {
                // Not an inline symlink. The path is 1 extent max, so just align to block size.
                size += alignN(uint64(len(hdr.Name)), blockSize)
            }
        default:
            size += inodeSize
        }

        err = tw.WriteHeader(hdr)
        if err != nil {
            goto AfterLoop
        }
        _, err = io.Copy(tw, tr)
        if err != nil {
            goto AfterLoop
        }
    }

AfterLoop:
    errClose := tw.Close()
    if err != nil {
        return nil, 0, 0, err
    }
    if errClose != nil {
        return nil, 0, 0, errClose
    }

    // Final adjustments to the size + inode
    // There are more metadata like Inode Table, block table, etc.
    // Let's just add 10% and call it good
    size, numInode = uint64(float64(size) * 1.10), uint64(float64(numInode) * 1.10)

    // Align to 64k
    if size % (64 * 1024) != 0 {
        size = alignN(size, 64*1024)
    }

    return rout, size, numInode, nil
}

func handleImportV2(conn *net.TCPConn) error {
    // First, copy the layer into memory & estimate the size of the tarfile.
    // TODO: could we just start with size = len(tarBUf)? and resize if we don't have space? 
    tarBuf, size, inodeNum, err := getBufAndEstimateSize(conn)
    if err != nil {
        fmt.Printf("Failed to estimate size of tar file: %s\n", err.Error())
    }
    fmt.Println(size, inodeNum)

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

    // Create loopback device.
    //fmt.Println("Creating loopback device")
    err = exec.Command("./create_fixed_vhd.sh", mntFolder, vhdFileName, strconv.FormatUint(size, 10), strconv.FormatUint(inodeNum, 10)).Run()
    if err != nil {
        fmt.Printf("error in create vhd script: %s\n", err.Error())
    }

    // Now unpack
    fmt.Printf("Unpacking to %s (dev %s)\n", mntFolder, vhdFileName)
    ret_size, _, err := tarlib.Unpack(tarBuf, mntFolder)
    if err != nil {
        fmt.Printf("error failed to unpack: %s\n", err.Error())
    }

    // now destroy looback device
    //fmt.Println("Unmounting device")
    err = exec.Command("/bin/umount", mntFolder).Run()
    if err != nil {
        fmt.Printf("error in unmounting disk: %s\n", err.Error())
    }

    // Now send back to the client
    // TODO: Actrually handle the error case, right now we just follow through and send success.
    fmt.Printf("Sending response with size: %d\n", ret_size)
    hdr := [4]byte{winlx.ResponseOKCmd, 0, 0, 0}
    buf := [8]byte{}
    binary.BigEndian.PutUint64(buf[:], ret_size)

    packet := append(hdr[:], buf[:]...)
    fmt.Println(packet, len(packet))

    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.ConnTimeOut)))
    _, err = conn.Write(packet)
    if err != nil {
        fmt.Printf("error in sending header packet: %s\n", err.Error())
        return err
    }

    // Send VHD file
    //fmt.Println("Opending VHD file")
    vhdFile2, err := os.Open(vhdFileName)
    if err != nil {
        fmt.Printf("error in opening vhd file\n")
    }
    defer vhdFile2.Close()

    //fmt.Println("Sending VHD File")
    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.WaitTimeOut)))
    _, err = io.Copy(conn, vhdFile2)
    if err != nil {
        fmt.Printf("error in transfering vhd back %s\n", err.Error())
        return err
    }

    // Send VHD footer
    //fmt.Println("Creating VHD footer")
    vhdr, err := vhd.NewVHDHeaderFixed(size)
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

    //fmt.Println("sending vhd footer")
    conn.SetWriteDeadline(time.Now().Add(time.Duration(time.Second * winlx.ConnTimeOut)))
    _, err = conn.Write(vhdrb.Bytes())
    if err != nil {
        fmt.Printf("error in sending VHD footer %s\n", err.Error())
        return err
    }

    // Send EOF
    conn.CloseWrite()
    return nil
}

func handleSingleClient(conn *net.TCPConn) error {
    hdr := [4]byte{}

    conn.SetReadDeadline(time.Now().Add(time.Duration(time.Second * winlx.WaitTimeOut)))
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

func ServiceVMAcceptClients(ip string) {
    addr, err := net.ResolveTCPAddr("tcp", ip)
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
    if len(os.Args) != 2 {
        fmt.Printf("Usage: %s <config_path>\n", os.Args[0])
        return
    }

    ip, err := parseConfig(os.Args[1])
    if err != nil {
        fmt.Printf("Error in parsing config: %s\n", err.Error())
        return
    }

    fmt.Printf("waiting for clients on %s...\n", ip)
    ServiceVMAcceptClients(ip)
}
