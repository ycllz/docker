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

	// Validate anything that came in through Config.Volumes (REST API caller) is valid
	for bind := range c.Volumes {
		if bind == "/" {
			return fmt.Errorf("Invalid volume: path can't be '/'")
		}
	}

	// Process each of the CLI-passed volumes
	for bind := range c.BCCLIVolumes {
		if arr := strings.Split(bind, ":"); len(arr) > 1 {
			if arr[1] == "/" {
				return fmt.Errorf("Invalid bind mount: destination can't be '/'")
			}
			if arr[0] == "" || arr[1] == "" {
				return fmt.Errorf("Invalid bind mount: Source or destination not supplied")
			}
			// after creating the bind mount we want to delete it from the
			// config.BCCLIVolumes values because we do not want bind mounts being
			// committed to image configs. However, it might also be a named volume.
			// As we know that binds start with /, we only move them to binds,
			// the others goto volumes.
			if string(bind[0]) == "/" {
				hc.Binds = append(hc.Binds, bind)
				delete(c.BCCLIVolumes, bind)
			} else {
				if _, ok := c.Volumes[bind]; ok {
					return fmt.Errorf("Duplicate volume spec %q was found", bind)
				}
				c.Volumes[bind] = c.BCCLIVolumes[bind]
				delete(c.BCCLIVolumes, bind)
			}
		} else if bind == "/" {
			// So we know it's a volume path as it didn't contain a colon
			return fmt.Errorf("Invalid volume: path can't be '/'")
		} else {
			// Also a volume path. Move it over to the config.Volumes structure
			// after ensuring it's not a duplicate.
			if _, ok := c.Volumes[bind]; ok {
				return fmt.Errorf("Duplicate volume spec %q was found", bind)
			}
			c.Volumes[bind] = c.BCCLIVolumes[bind]
			delete(c.BCCLIVolumes, bind)
		}
	}

	// Now we need to validate that anything that was moved over to
	// binds is actually a bind (as in one which can be parsed, meets
	// critieria such as destination not being /, and that has a source
	// (which by definition, a bind must have a source).
	for _, bind := range hc.Binds {
		mp, err := volume.ParseMountSpec(bind, hc.VolumeDriver)
		if err != nil {
			return fmt.Errorf("Unrecognised bind spec: %v", err)
		}
		// A bind must have a source
		if len(mp.Source) == 0 {
			return fmt.Errorf("No source specified for bind spec %q", bind)
		}
	}

	return nil
}
