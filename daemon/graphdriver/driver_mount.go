package graphdriver

import (
	"github.com/containerd/continuity/driver"
	"github.com/containerd/continuity/pathdriver"
)

// Mount is that represents a mount point and provides functions to
// manipulate files & paths.
type Mount interface {
	// Path returns the path to the mount point. Note that this may not exist
	// on the local system, so the continuity operations must be used
	Path() string

	// Driver & PathDriver provide methods to manipulate files & paths
	driver.Driver
	pathdriver.PathDriver
}

// NewLocalMount is a helper function to implement daemon's Mount interface
// when the graphdriver mount point is a local path on the machine.
func NewLocalMount(path string) Mount {
	return &localMount{
		path:       path,
		Driver:     driver.LocalDriver,
		PathDriver: pathdriver.LocalPathDriver,
	}
}

type localMount struct {
	path string
	driver.Driver
	pathdriver.PathDriver
}

func (l *localMount) Path() string {
	return l.path
}
