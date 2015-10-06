// +build !windows

package runconfig

import (
	"fmt"
	"strings"

	"github.com/docker/docker/volume"
)

// ContainerConfigWrapper is a Config wrapper that hold the container Config (portable)
// and the corresponding HostConfig (non-portable).
type ContainerConfigWrapper struct {
	*Config
	InnerHostConfig *HostConfig `json:"HostConfig,omitempty"`
	Cpuset          string      `json:",omitempty"` // Deprecated. Exported for backwards compatibility.
	*HostConfig                 // Deprecated. Exported to read attributes from json that are not in the inner host config structure.
}

// getHostConfig gets the HostConfig of the Config.
// It's mostly there to handle Deprecated fields of the ContainerConfigWrapper
func (w *ContainerConfigWrapper) getHostConfig() *HostConfig {
	hc := w.HostConfig

	if hc == nil && w.InnerHostConfig != nil {
		hc = w.InnerHostConfig
	} else if w.InnerHostConfig != nil {
		if hc.Memory != 0 && w.InnerHostConfig.Memory == 0 {
			w.InnerHostConfig.Memory = hc.Memory
		}
		if hc.MemorySwap != 0 && w.InnerHostConfig.MemorySwap == 0 {
			w.InnerHostConfig.MemorySwap = hc.MemorySwap
		}
		if hc.CPUShares != 0 && w.InnerHostConfig.CPUShares == 0 {
			w.InnerHostConfig.CPUShares = hc.CPUShares
		}
		if hc.CpusetCpus != "" && w.InnerHostConfig.CpusetCpus == "" {
			w.InnerHostConfig.CpusetCpus = hc.CpusetCpus
		}

		if hc.VolumeDriver != "" && w.InnerHostConfig.VolumeDriver == "" {
			w.InnerHostConfig.VolumeDriver = hc.VolumeDriver
		}

		hc = w.InnerHostConfig
	}

	if hc != nil {
		if w.Cpuset != "" && hc.CpusetCpus == "" {
			hc.CpusetCpus = w.Cpuset
		}
	}

	// Make sure NetworkMode has an acceptable value. We do this to ensure
	// backwards compatible API behaviour.
	hc = SetDefaultNetModeIfBlank(hc)

	return hc
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
// everything supplied by the user in the config.BCCLIVolumes field (Backwards-
// Compatible CLI). However, any pre-existing REST API caller can continue to
// do as it did before by passing information in through either config.Volumes
// or HostConfig.Binds.
func processVolumesAndBindSettings(c *Config, hc *HostConfig) error {
	for spec := range c.BCCLIVolumes {
		if arr := strings.Split(spec, ":"); len(arr) > 1 {
			// A bind mount, not a volume.
			if arr[0] == "" || arr[1] == "" {
				return fmt.Errorf("Invalid bind mount %q: Source or destination not supplied", spec)
			}
			// Move to binds as we do not want bind mounts committed to image configs.
			hc.Binds = append(hc.Binds, spec)
			delete(c.BCCLIVolumes, spec)
		} else {
			// A volume - move to config.Volumes
			c.Volumes[spec] = c.BCCLIVolumes[spec]
			delete(c.BCCLIVolumes, spec)
		}
	}

	// Ensure all volumes and binds are valid.
	for spec := range c.Volumes {
		if _, err := volume.ParseMountSpec(spec, hc.VolumeDriver); err != nil {
			return fmt.Errorf("Invalid volume spec %q: %v", spec, err)
		}
	}
	for _, spec := range hc.Binds {
		if _, err := volume.ParseMountSpec(spec, hc.VolumeDriver); err != nil {
			return fmt.Errorf("Invalid bind mount spec %q: %v", spec, err)
		}
	}

	return nil
}
