package runconfig

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/pkg/nat"
	"github.com/docker/docker/pkg/stringutils"
)

// Config contains the configuration data about a container.
// It should hold only portable information about the container.
// Here, "portable" means "independent from the host we are running on".
// Non-portable information *should* appear in HostConfig.
type Config struct {
	Hostname        string                // Hostname
	Domainname      string                // Domainname
	User            string                // User that will run the command(s) inside the container
	AttachStdin     bool                  // Attach the standard input, makes possible user interaction
	AttachStdout    bool                  // Attach the standard output
	AttachStderr    bool                  // Attach the standard error
	ExposedPorts    map[nat.Port]struct{} // List of exposed ports
	PublishService  string                // Name of the network service exposed by the container
	Tty             bool                  // Attach standard streams to a tty, including stdin if it is not closed.
	OpenStdin       bool                  // Open stdin
	StdinOnce       bool                  // If true, close stdin after the 1 attached client disconnects.
	Env             []string              // List of environment variable to set in the container
	Cmd             *stringutils.StrSlice // Command to run when starting the container
	Image           string                // Name of the image as it was passed by the operator (eg. could be symbolic)
	Volumes         map[string]struct{}   // List of volumes (mounts) used for the container
	BCCLIVolumes    map[string]struct{}   // Back compat CLI-passed list of volumes (mounts) used for the container
	WorkingDir      string                // Current directory (PWD) in the command will be launched
	Entrypoint      *stringutils.StrSlice // Entrypoint to run when starting the container
	NetworkDisabled bool                  // Is network disabled
	MacAddress      string                // Mac Address of the container
	OnBuild         []string              // ONBUILD metadata that were defined on the image Dockerfile
	Labels          map[string]string     // List of labels set to this container
	StopSignal      string                // Signal to stop a container
}

// DecodeContainerConfig decodes a json encoded config into a ContainerConfigWrapper
// struct and returns both a Config and an HostConfig struct
// Be aware this function is not checking whether the resulted structs are nil,
// it's your business to do so
func DecodeContainerConfig(src io.Reader) (*Config, *HostConfig, error) {
	var w ContainerConfigWrapper

	decoder := json.NewDecoder(src)
	if err := decoder.Decode(&w); err != nil {
		return nil, nil, err
	}

	hc := w.getHostConfig()

	// Perform platform-specific processing of Volumes and Binds.
	if w.Config != nil && hc != nil {

		// Validate that if called from a REST API caller (where volumes and/or
		// binds might be set) that the backwards-compatible field that the
		// client uses is not set.
		if len(w.Config.BCCLIVolumes) > 0 &&
			((len(w.Config.Volumes) > 0) || len(hc.Binds) > 0) {
			return nil, nil, fmt.Errorf("Binds or Volumes cannot be supplied with BCCLIVolumes")
		}

		// Initialise the volumes map if currently nil
		if w.Config.Volumes == nil {
			w.Config.Volumes = make(map[string]struct{})
		}

		// Now do the platform-specific processing
		if err := processVolumesAndBindSettings(w.Config, hc); err != nil {
			return nil, nil, err
		}
	}

	// Certain parameters need daemon-side validation that cannot be done
	// on the client, as only the daemon knows what is valid for the platform.
	if err := ValidateNetMode(w.Config, hc); err != nil {
		return nil, nil, err
	}

	return w.Config, hc, nil
}
