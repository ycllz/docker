// +build windows

package container

import (
	"fmt"

	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/docker/utils"
	"github.com/docker/docker/volume"
	"github.com/docker/engine-api/types/container"
)

// Container holds fields specific to the Windows implementation. See
// CommonContainer for standard fields common to all containers.
type Container struct {
	CommonContainer

	// Fields below here are platform specific.
}

// CreateDaemonEnvironment sets the environment. It is effectively a hack
// to allow ENV Path=c:\somepath;$Path in builder.
func (container *Container) CreateDaemonEnvironment(linkedEnv []string) []string {
	// Setup environment
	env := []string{"Path=" + system.DefaultPathEnv}

	// because the env on the container can override certain default values
	// we need to replace the 'env' keys where they match and append anything
	// else.
	fmt.Println("Before: ", env)
	fmt.Println("CurrentEnv", container.Config.Env)
	env = utils.ReplaceOrAppendEnvValues(env, container.Config.Env)
	fmt.Println("After: ", env)

	return env
}

// SetupWorkingDirectory initializes the container working directory.
// This is a NOOP In windows.
func (container *Container) SetupWorkingDirectory() error {
	return nil
}

// UnmountIpcMounts unmount Ipc related mounts.
// This is a NOOP on windows.
func (container *Container) UnmountIpcMounts(unmount func(pth string) error) {
}

// IpcMounts returns the list of Ipc related mounts.
func (container *Container) IpcMounts() []execdriver.Mount {
	return nil
}

// UnmountVolumes explicitly unmounts volumes from the container.
func (container *Container) UnmountVolumes(forceSyscall bool, volumeEventLog func(name, action string, attributes map[string]string)) error {
	return nil
}

// TmpfsMounts returns the list of tmpfs mounts
func (container *Container) TmpfsMounts() []execdriver.Mount {
	return nil
}

// UpdateContainer updates resources of a container
func (container *Container) UpdateContainer(hostConfig *container.HostConfig) error {
	return nil
}

// appendNetworkMounts appends any network mounts to the array of mount points passed in.
// Windows does not support network mounts (not to be confused with SMB network mounts), so
// this is a no-op.
func appendNetworkMounts(container *Container, volumeMounts []volume.MountPoint) ([]volume.MountPoint, error) {
	return volumeMounts, nil
}
