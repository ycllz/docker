package runconfig

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/pkg/nat"
	"github.com/docker/docker/pkg/stringutils"
	"github.com/docker/docker/volume"
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

	// As the CLI does not know the daemon platform, and volumes/bind mounts
	// can only be accurately parsed on the daemon side, we need to
	// parse them here, and move any volumes which come in in Config.Volumes
	// into HostConfig.Binds.
	if w.Config != nil && hc != nil {
		for bind := range w.Config.Volumes {
			mp, err := volume.ParseMountSpec(bind, hc.VolumeDriver)
			if err != nil {
				return nil, nil, fmt.Errorf("Unrecognised volume spec: %v", err)
			}
			if len(mp.Source) > 0 {
				// After creating the bind mount (one in which a host directory is specified),
				// we want to delete it from the config.Volumes values because we do not want
				// bind mounts being committed to image configs.
				// Note the spec can be one of source:destination:mode, destination,
				// or source:destination
				hc.Binds = append(hc.Binds, bind)
				delete(w.Config.Volumes, bind)
			}
		}
	}

	// Certain parameters need daemon-side validation that cannot be done
	// on the client, as only the daemon knows what is valid for the platform.
	if err := ValidateNetMode(w.Config, hc); err != nil {
		return nil, nil, err
	}

	return w.Config, hc, nil
}
