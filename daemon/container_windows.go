// +build windows

package daemon

import (
	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/volume"
)

// DefaultPathEnv is deliberately empty on Windows as the default path will be set by
// the container. Docker has no context of what the default path should be.
const DefaultPathEnv = ""

// Container holds fields specific to the Windows implementation. See
// CommonContainer for standard fields common to all containers.
type Container struct {
	CommonContainer

	// Fields below here are platform specific.
}

func killProcessDirectly(container *Container) error {
	return nil
}

func (container *Container) setupLinkedContainers() ([]string, error) {
	return nil, nil
}

func (container *Container) createDaemonEnvironment(linkedEnv []string) []string {
	// On Windows, nothing to link. Just return the container environment.
	return container.Config.Env
}

func (container *Container) initializeNetworking() error {
	return nil
}

// ConnectToNetwork connects a container to the network
func (container *Container) ConnectToNetwork(idOrName string) error {
	return nil
}

func (container *Container) setupWorkingDirectory() error {
	return nil
}

func populateCommand(c *Container, env []string) error {
	return nil
}

// GetSize returns real size & virtual size
func (container *Container) getSize() (int64, int64) {
	// TODO Windows
	return 0, 0
}

// setNetworkNamespaceKey is a no-op on Windows.
func (container *Container) setNetworkNamespaceKey(pid int) error {
	return nil
}

// allocateNetwork is a no-op on Windows.
func (container *Container) allocateNetwork() error {
	return nil
}

func (container *Container) updateNetwork() error {
	return nil
}

func (container *Container) releaseNetwork() {
}

// appendNetworkMounts appends any network mounts to the array of mount points passed in.
// Windows does not support network mounts (not to be confused with SMB network mounts), so
// this is a no-op.
func appendNetworkMounts(container *Container, volumeMounts []volume.MountPoint) ([]volume.MountPoint, error) {
	return volumeMounts, nil
}

func (container *Container) setupIpcDirs() error {
	return nil
}

func (container *Container) unmountIpcMounts() error {
	return nil
}

func (container *Container) ipcMounts() []execdriver.Mount {
	return nil
}

func getDefaultRouteMtu() (int, error) {
	return -1, errSystemNotSupported
}
