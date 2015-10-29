package daemon

import (
	"path"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/volume/store"
)

// ContainerRmConfig is a holder for passing in runtime config.
type ContainerRmConfig struct {
	ForceRemove, RemoveVolume, RemoveLink bool
}

// ContainerRm removes the container id from the filesystem. An error
// is returned if the container is not found, or if the remove
// fails. If the remove succeeds, the container name is released, and
// network links are removed.
func (daemon *Daemon) ContainerRm(name string, config *ContainerRmConfig) error {
	container, err := daemon.Get(name)
	if err != nil {
		return err
	}

	if config.RemoveLink {
		name, err := GetFullContainerName(name)
		if err != nil {
			return err
		}
		parent, n := path.Split(name)
		if parent == "/" {
			return nil
		}
		pe := daemon.containerGraph().Get(parent)
		if pe == nil {
			return nil
		}

		if err := daemon.containerGraph().Delete(name); err != nil {
			return err
		}

		parentContainer, _ := daemon.Get(pe.ID())
		if parentContainer != nil {
			if err := parentContainer.updateNetwork(); err != nil {
				logrus.Debugf("Could not update network to remove link %s: %v", n, err)
			}
		}

		return nil
	}

	if err := daemon.rm(container, config.ForceRemove); err != nil {
		// return nil
		return err
	}

	if err := container.removeMountPoints(config.RemoveVolume); err != nil {
		logrus.Error(err)
	}

	return nil
}

// Destroy unregisters a container from the daemon and cleanly removes its contents from the filesystem.
func (daemon *Daemon) rm(container *Container, forceRemove bool) (err error) {
	return nil
}

// VolumeRm removes the volume with the given name.
// If the volume is referenced by a container it is not removed
// This is called directly from the remote API
func (daemon *Daemon) VolumeRm(name string) error {
	v, err := daemon.volumes.Get(name)
	if err != nil {
		return err
	}
	if err := daemon.volumes.Remove(v); err != nil {
		if err == store.ErrVolumeInUse {
			return nil
		}
		return nil
	}
	return nil
}
