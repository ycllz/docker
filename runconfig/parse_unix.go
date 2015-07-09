// +build !windows

package runconfig

import (
	"fmt"
	"strings"
)

func ValidateNetMode(c *Config, hc *HostConfig) error {
	// In some circumstances, we may not be passed a host config, such as
	// in the case of docker commit
	if hc == nil {
		return nil
	}
	parts := strings.Split(string(hc.NetworkMode), ":")
	switch mode := parts[0]; mode {
	case "default", "bridge", "none", "host":
	case "container":
		if len(parts) < 2 || parts[1] == "" {
			return fmt.Errorf("--net: invalid net mode: invalid container format container:<name|id>")
		}
	default:
		return fmt.Errorf("invalid --net: %s", hc.NetworkMode)
	}

	if (hc.NetworkMode.IsHost() || hc.NetworkMode.IsContainer()) && c.Hostname != "" {
		return ErrConflictNetworkHostname
	}

	if hc.NetworkMode.IsHost() && len(hc.Links) > 0 {
		return ErrConflictHostNetworkAndLinks
	}

	if hc.NetworkMode.IsContainer() && len(hc.Links) > 0 {
		return ErrConflictContainerNetworkAndLinks
	}

	if (hc.NetworkMode.IsHost() || hc.NetworkMode.IsContainer()) && len(hc.Dns) > 0 {
		return ErrConflictNetworkAndDns
	}

	if (hc.NetworkMode.IsContainer() || hc.NetworkMode.IsHost()) && len(hc.ExtraHosts) > 0 {
		return ErrConflictNetworkHosts
	}

	if (hc.NetworkMode.IsContainer() || hc.NetworkMode.IsHost()) && c.MacAddress != "" {
		return ErrConflictContainerNetworkAndMac
	}

	if hc.NetworkMode.IsContainer() && (hc.ExplicitPublishPorts || hc.PublishAllPorts == true) {
		return ErrConflictNetworkPublishPorts
	}

	if hc.NetworkMode.IsContainer() && hc.ExplicitExposedPorts {
		return ErrConflictNetworkExposePorts
	}
	return nil
}
