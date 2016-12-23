package tarlib

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
    "unsafe"
    "time"
)

// LUtimesNano is used to change access and modification time of the specified path.
// It's used for symbol link file because syscall.UtimesNano doesn't support a NOFOLLOW flag atm.
func LUtimesNano(path string, ts []syscall.Timespec) error {
    // These are not currently available in syscall
    atFdCwd := -100
    atSymLinkNoFollow := 0x100

    var _path *byte
    _path, err := syscall.BytePtrFromString(path)
    if err != nil {
        return err
    }

    if _, _, err := syscall.Syscall6(syscall.SYS_UTIMENSAT, uintptr(atFdCwd), uintptr(unsafe.Pointer(_path)), uintptr(unsafe.Pointer(&ts[0])), uintptr(atSymLinkNoFollow), 0, 0); err != 0 && err != syscall.ENOSYS {
        return err
    }

    return nil
}

func timeToTimespec(time time.Time) (ts syscall.Timespec) {
    if time.IsZero() {
        // Return UTIME_OMIT special value
        ts.Sec = 0
        ts.Nsec = ((1 << 30) - 2)
        return
    }
    return syscall.NsecToTimespec(time.UnixNano())
}

func makeDev(major, minor int64) int {
    return int((major << 8) | minor)
}

func handleLChmod(hdr *tar.Header, path string, hdrInfo os.FileInfo) error {
    if hdr.Typeflag == tar.TypeLink {
        if fi, err := os.Lstat(hdr.Linkname); err == nil && (fi.Mode()&os.ModeSymlink == 0) {
            if err := os.Chmod(path, hdrInfo.Mode()); err != nil {
                return err
            }
        }
    } else if hdr.Typeflag != tar.TypeSymlink {
        if err := os.Chmod(path, hdrInfo.Mode()); err != nil {
            return err
        }
    }
    return nil
}

// handleTarTypeBlockCharFifo is an OS-specific helper function used by
// createTarFile to handle the following types of header: Block; Char; Fifo
func handleTarTypeBlockCharFifo(hdr *tar.Header, path string) error {
    mode := uint32(hdr.Mode & 07777)
    switch hdr.Typeflag {
    case tar.TypeBlock:
        mode |= syscall.S_IFBLK
    case tar.TypeChar:
        mode |= syscall.S_IFCHR
    case tar.TypeFifo:
        mode |= syscall.S_IFIFO
    }

    if err := syscall.Mknod(path, mode, int(makeDev(hdr.Devmajor, hdr.Devminor))); err != nil {
        return err
    }
    return nil
}

func createTarFile(path, extractDir string, hdr *tar.Header, reader io.Reader) (uint64, error) {
    // hdr.Mode is in linux format, which we can use for sycalls,
    // but for os.Foo() calls we need the mode converted to os.FileMode,
    // so use hdrInfo.Mode() (they differ for e.g. setuid bits)
    hdrInfo := hdr.FileInfo()
    var written uint64 = 0

    switch hdr.Typeflag {
    case tar.TypeDir:
        // Create directory unless it exists as a directory already.
        // In that case we just want to merge the two
        if fi, err := os.Lstat(path); !(err == nil && fi.IsDir()) {
            if err := os.Mkdir(path, hdrInfo.Mode()); err != nil {
                fmt.Printf("failed mkdir %s\n", path)
                return 0, err
            }
        }

    case tar.TypeReg, tar.TypeRegA:
        // Source is regular file. We use system.OpenFileSequential to use sequential
        // file access to avoid depleting the standby list on Windows.
        // On Linux, this equates to a regular os.OpenFile
        file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, hdrInfo.Mode())
        if err != nil {
            fmt.Printf("faild open regular file: %s\n", path)
            return 0, err
        }

        if _, err := io.Copy(file, reader); err != nil {
            fmt.Printf("failed copy regular file: %s\n", path)
            file.Close()
            return 0, err
        }
        file.Close()
        written += uint64(hdr.Size)

    case tar.TypeBlock, tar.TypeChar:
        // Handle this is an OS-specific way
        if err := handleTarTypeBlockCharFifo(hdr, path); err != nil {
            fmt.Printf("failed create device: %s\n", path);
            return 0, err
        }

    case tar.TypeFifo:
        // Handle this is an OS-specific way
        if err := handleTarTypeBlockCharFifo(hdr, path); err != nil {
            fmt.Printf("failed create fifo: %s\n", path)
            return 0, err
        }

    case tar.TypeLink:
        targetPath := filepath.Join(extractDir, hdr.Linkname)
        // check for hardlink breakout
        if !strings.HasPrefix(targetPath, extractDir) {
            return 0, fmt.Errorf("invalid hardlink %q -> %q", targetPath, hdr.Linkname)
        }
        if err := os.Link(targetPath, path); err != nil {
            fmt.Printf("failed hard link: %s -> %s\n", path, targetPath)
            return 0, err
        }

    case tar.TypeSymlink:
        //  path                -> hdr.Linkname = targetPath
        // e.g. /extractDir/path/to/symlink     -> ../2/file    = /extractDir/path/2/file
        targetPath := filepath.Join(filepath.Dir(path), hdr.Linkname)

        // the reason we don't need to check symlinks in the path (with FollowSymlinkInScope) is because
        // that symlink would first have to be created, which would be caught earlier, at this very check:
        if !strings.HasPrefix(targetPath, extractDir) {
            return 0, fmt.Errorf("invalid symlink %q -> %q", path, hdr.Linkname)
        }
        if err := os.Symlink(hdr.Linkname, path); err != nil {
            fmt.Printf("failed symlink: %s -> %s\n", path, targetPath)
            return 0, err
        }

    default:
        return 0, fmt.Errorf("Unhandled tar header type %d\n", hdr.Typeflag)
    }

    // Lchown is noddt supported on Windows.
     if err := os.Lchown(path, int(hdr.Uid), int(hdr.Gid)); err != nil {
        fmt.Printf("failed lchown: %s\n", path)
        return 0, err
     }

    // There is no LChmod, so ignore mode for symlink. Also, this
    // must happen after chown, as that can modify the file mode
    if err := handleLChmod(hdr, path, hdrInfo); err != nil {
        fmt.Printf("failed chmod: %s\n", path)
        return 0, err
    }

    aTime := hdr.AccessTime
    if aTime.Before(hdr.ModTime) {
        // Last access time should never be before last modified time.
        aTime = hdr.ModTime
    }

    // system.Chtimes doesn't support a NOFOLLOW flag atm
    if hdr.Typeflag == tar.TypeLink {
        if fi, err := os.Lstat(hdr.Linkname); err == nil && (fi.Mode()&os.ModeSymlink == 0) {
            if err := Chtimes(path, aTime, hdr.ModTime); err != nil {
                fmt.Printf("failed hardlink chtimes: %s\n", path)
                return 0, err
            }
        }
    } else if hdr.Typeflag != tar.TypeSymlink {
        if err := Chtimes(path, aTime, hdr.ModTime); err != nil {
            fmt.Printf("failed non link chtimes: %s\n", path)
            return 0, err
        }
    } else {
        ts := []syscall.Timespec{timeToTimespec(aTime), timeToTimespec(hdr.ModTime)}
        if err := LUtimesNano(path, ts); err != nil {
            fmt.Printf("failed symlink chtimes: %s\n", path)
            return 0, err
        }
    }
    return written, nil
}

// Unpack unpacks the decompressedArchive to dest with options.
func Unpack(decompressedArchive io.Reader, dest string) (uint64, error) {
	tr := tar.NewReader(decompressedArchive)

	var dirs []*tar.Header

	// Iterate through the files in the archive.
	var size uint64 = 0
    for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
		    return 0, err
		}

        fmt.Println(hdr.Name)

		// Normalize name, for safety and for a simple is-root check
		// This keeps "../" as-is, but normalizes "/../" to "/". Or Windows:
		// This keeps "..\" as-is, but normalizes "\..\" to "\".
		hdr.Name = filepath.Clean(hdr.Name)

		// After calling filepath.Clean(hdr.Name) above, hdr.Name will now be in
		// the filepath format for the OS on which the daemon is running. Hence
		// the check for a slash-suffix MUST be done in an OS-agnostic way.
		if !strings.HasSuffix(hdr.Name, string(os.PathSeparator)) {
			// Not the root directory, ensure that the parent directory exists
			parent := filepath.Dir(hdr.Name)
			parentPath := filepath.Join(dest, parent)
			if _, err := os.Lstat(parentPath); err != nil && os.IsNotExist(err) {
                err = os.MkdirAll(parentPath, os.FileMode(0777))
                if err != nil {
                    return 0, err
                }
			}
		}

		path := filepath.Join(dest, hdr.Name)
		rel, err := filepath.Rel(dest, path)
		if err != nil {
            return 0, err
		}
		if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return 0, fmt.Errorf("%q is outside of %q", hdr.Name, dest)
		}

		// If path exits we almost always just want to remove and replace it
		// The only exception is when it is a directory *and* the file from
		// the layer is also a directory. Then we want to merge them (i.e.
		// just apply the metadata from the layer).
		if fi, err := os.Lstat(path); err == nil {
			if fi.IsDir() && hdr.Name == "." {
				continue
			}

			if !(fi.IsDir() && hdr.Typeflag == tar.TypeDir) {
				if err := os.RemoveAll(path); err != nil {
			        return 0, err
				}
			}
		}

        written, err := createTarFile(path, dest, hdr, tr)
        if err != nil {
            return 0, err
		}
        size += written

		// Directory mtimes must be handled at the end to avoid further
		// file creation in them to modify the directory mtime
		if hdr.Typeflag == tar.TypeDir {
			dirs = append(dirs, hdr)
		}
	}

	for _, hdr := range dirs {
		path := filepath.Join(dest, hdr.Name)

		if err := Chtimes(path, hdr.AccessTime, hdr.ModTime); err != nil {
            return 0, err
		}
	}
	return size, nil
}
