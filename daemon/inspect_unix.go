// +build !windows

package daemon

import "github.com/docker/docker/api/types"

// This sets platform-specific fields
func setPlatformSpecificContainerFields(container *Container, contJSONBase *types.ContainerJSONBase) *types.ContainerJSONBase {

	volumes := make(map[string]string)
	volumesRW := make(map[string]bool)

	for _, m := range container.MountPoints {
		volumes[m.Destination] = m.Path()
		volumesRW[m.Destination] = m.RW
	}

	contJSONBase.AppArmorProfile = container.AppArmorProfile
	contJSONBase.ResolvConfPath = container.ResolvConfPath
	contJSONBase.HostnamePath = container.HostnamePath
	contJSONBase.HostsPath = container.HostsPath
	contJSONBase.Volumes = volumes
	contJSONBase.VolumesRW = volumesRW

	return contJSONBase
}
