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
	case "default", "none":
	default:
		return fmt.Errorf("invalid --net: %s", hc.NetworkMode)
	}
	return nil
}
