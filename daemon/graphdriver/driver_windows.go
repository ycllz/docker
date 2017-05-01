package graphdriver

import (
	"github.com/docker/docker/pkg/system"
)

var (
	// Slice of drivers that should be used in order
	priority = []string{"windowsfilter"}
)

// GetFSMagic returns the filesystem id given the path.
func GetFSMagic(rootpath string) (FsMagic, error) {
	// Note it is OK to return FsMagicUnsupported on Windows.
	return FsMagicUnsupported, nil
}

func init() {
	if system.LCOWSupported() {
		priority = append(priority, "lcow")
	}
}
