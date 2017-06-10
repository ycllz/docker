package system

import (
	"fmt"
	"path/filepath"
	"strings"

	"runtime"

	"github.com/containerd/continuity/pathdriver"
)

// CheckSystemDriveAndRemoveDriveLetter verifies and manipulates a Windows or
// Linux path.
// On Linux paths, it noops.
// On Windows paths, it verifies and manipulates a Windows path.
// This is used, for example, when validating a user provided path in docker cp.
// If a drive letter is supplied, it must be the system drive. The drive letter
// is always removed. Also, it translates it to OS semantics (IOW / to \). We
// need the path in this syntax so that it can ultimately be contatenated with
// a Windows long-path which doesn't support drive-letters. Examples:
// C:			--> Fail
// C:\			--> \
// a			--> a
// /a			--> \a
// d:\			--> Fail
func CheckSystemDriveAndRemoveDriveLetter(path string, driver pathdriver.PathDriver) (string, error) {
	if runtime.GOOS != "windows" {
		return path, nil
	}

	if len(path) == 2 && string(path[1]) == ":" {
		return "", fmt.Errorf("No relative path specified in %q", path)
	}
	if !driver.IsAbs(path) || len(path) < 2 {
		return filepath.FromSlash(path), nil
	}
	if string(path[1]) == ":" && !strings.EqualFold(string(path[0]), "c") {
		return "", fmt.Errorf("The specified path is not on the system drive (C:)")
	}
	return filepath.FromSlash(path[2:]), nil
}
