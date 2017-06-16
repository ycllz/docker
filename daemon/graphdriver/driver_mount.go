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

// LocalGetFunc is a function definition that is identical if the file system
// exists locally on the host.
type LocalGetFunc func(string, string) (string, error)

// WrapLocalGetFunc wraps the old graphdriver Get() interface (LocalGetFunc)
// with the current graphdriver.Get() interface
func WrapLocalGetFunc(id, mountLabel string, f LocalGetFunc) (Mount, error) {
	mnt, err := f(id, mountLabel)
	if err != nil {
		return nil, err
	}
	return NewLocalMount(mnt), nil
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
