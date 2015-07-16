package daemon

import (
	"github.com/docker/docker/runconfig"
)

// createPlatformSpecific performs platform specific container create functionality
func createPlatformSpecific(container *Container, config *runconfig.Config) error {
	return nil
}
