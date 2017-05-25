package system

import (
	"fmt"
	"path/filepath"
	"strings"

	"runtime"

	"github.com/containerd/continuity/fsdriver"
)

// CheckSystemDriveAndRemoveDriveLetterOS verifies and manipulates a Windows or
// Linux path.
func CheckSystemDriveAndRemoveDriveLetterOS(path string, driver fsdriver.Driver) (string, error) {
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
