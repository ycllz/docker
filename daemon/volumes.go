package daemon

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/pkg/chrootarchive"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/docker/runconfig"
	"github.com/docker/docker/volume"
)

// TODO Windows. Further platform refactoring can still be done in volumes*.go

type mountPoint struct {
	Name        string
	Destination string
	Driver      string
	RW          bool
	Volume      volume.Volume `json:"-"`
	Source      string
	Relabel     string
}

func (m *mountPoint) Setup() (string, error) {
	if m.Volume != nil {
		return m.Volume.Mount()
	}

	if len(m.Source) > 0 {
		if _, err := os.Stat(m.Source); err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			if err := system.MkdirAll(m.Source, 0755); err != nil {
				return "", err
			}
		}
		return m.Source, nil
	}

	return "", fmt.Errorf("Unable to setup mount point, neither source nor volume defined")
}

func (m *mountPoint) Path() string {
	if m.Volume != nil {
		return m.Volume.Path()
	}

	return m.Source
}

// BackwardsCompatible decides whether this mount point can be
// used in old versions of Docker or not.
// Only bind mounts and local volumes can be used in old versions of Docker.
func (m *mountPoint) BackwardsCompatible() bool {
	return len(m.Source) > 0 || m.Driver == volume.DefaultDriverName
}

func parseBindMount(spec string, mountLabel string, config *runconfig.Config) (*mountPoint, error) {
	bind := &mountPoint{
		RW: true,
	}
	arr := strings.Split(spec, ":")

	switch len(arr) {
	case 2:
		bind.Destination = arr[1]
	case 3:
		bind.Destination = arr[1]
		mode := arr[2]
		isValid, isRw := volume.ValidateMountMode(mode)
		if !isValid {
			return nil, fmt.Errorf("invalid mode for volumes-from: %s", mode)
		}
		bind.RW = isRw
		// Relabel will apply a SELinux label, if necessary
		bind.Relabel = mode
	default:
		return nil, fmt.Errorf("Invalid volume specification: %s", spec)
	}

	name, source, err := parseVolumeSource(arr[0])
	if err != nil {
		return nil, err
	}

	if len(source) == 0 {
		bind.Driver = config.VolumeDriver
		if len(bind.Driver) == 0 {
			bind.Driver = volume.DefaultDriverName
		}
	} else {
		bind.Source = filepath.Clean(source)
	}

	bind.Name = name
	bind.Destination = filepath.Clean(bind.Destination)
	return bind, nil
}

func parseVolumesFrom(spec string) (string, string, error) {
	if len(spec) == 0 {
		return "", "", fmt.Errorf("malformed volumes-from specification: %s", spec)
	}

	specParts := strings.SplitN(spec, ":", 2)
	id := specParts[0]
	mode := "rw"

	if len(specParts) == 2 {
		mode = specParts[1]
		if isValid, _ := volume.ValidateMountMode(mode); !isValid {
			return "", "", fmt.Errorf("invalid mode for volumes-from: %s", mode)
		}
	}
	return id, mode, nil
}

func copyExistingContents(source, destination string) error {
	volList, err := ioutil.ReadDir(source)
	if err != nil {
		return err
	}
	if len(volList) > 0 {
		srcList, err := ioutil.ReadDir(destination)
		if err != nil {
			return err
		}
		if len(srcList) == 0 {
			// If the source volume is empty copy files from the root into the volume
			if err := chrootarchive.CopyWithTar(source, destination); err != nil {
				return err
			}
		}
	}
	return copyOwnership(source, destination)
}
