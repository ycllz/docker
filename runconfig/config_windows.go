package runconfig

import (
	"fmt"

	"github.com/docker/docker/volume"
)

// ContainerConfigWrapper is a Config wrapper that hold the container Config (portable)
// and the corresponding HostConfig (non-portable).
type ContainerConfigWrapper struct {
	*Config
	HostConfig *HostConfig `json:"HostConfig,omitempty"`
}

// getHostConfig gets the HostConfig of the Config.
func (w *ContainerConfigWrapper) getHostConfig() *HostConfig {
	return w.HostConfig
}

// processVolumesAndBindSettings processes the volumes and bind settings
// which are received from the caller (docker CLI or REST API) in a platform
// specific manner.
//
// This is necessary due to platform specifics, where the spec supplied
// cannot be parsed accurately by the client as it doesn't know the daemon
// platform where the spec is relevant.
//
// To ensure backwards compatibility of the REST API, the docker CLI passes
// everything supplied by the user in the config.CLIVolumeSpecs field (used by
// CLI). However, any pre-existing REST API caller can continue to
// do as it did before by passing information in through either config.Volumes
// or HostConfig.Binds.
func processVolumesAndBindSettings(c *Config, hc *HostConfig) error {
	// Move everything from the backwards compatibility structure into volumes.
	// We don't need to worry about potentially overwriting anything as previous
	// to us being called, we are guaranteed that if CLIVolumeSpecs is populated,
	// then Volumes not populated. However, don't overwrite if Volumes is
	// populated as that could be from a REST API caller.
	if len(c.Volumes) == 0 {
		c.Volumes = c.CLIVolumeSpecs
	}
	c.CLIVolumeSpecs = nil

	// Move bind mounts from config.Volumes into hc.Binds as we do not want
	// them committed to image configs.
	for bind := range c.Volumes {
		mp, err := volume.ParseMountSpec(bind, hc.VolumeDriver)
		if err != nil {
			return fmt.Errorf("Unrecognised volume spec: %v", err)
		}
		if len(mp.Source) > 0 {
			hc.Binds = append(hc.Binds, bind)
			delete(c.Volumes, bind)
		}
	}

	return nil
}
