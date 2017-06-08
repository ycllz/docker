package opengcs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
)

type Mode string

const (
	ModeError        = "Invalid configuration mode"
	ModeVhdx         = "VHDX mode"
	ModeKernelInitrd = "Kernel/Initrd mode"
)

// Config is the structure used to configuring a utility VM to be used
// as a service VM. There are two ways of starting. Either supply a VHD,
// or a Kernel+Initrd. For the latter, both must be supplied, and both
// must be in the same directory.
//
// VHD is the priority.
//
// All paths are full host path-names.
//
// TODO @jhowardmsft Platform change in flight - for k+i, currently the
// utilities VHD path is also required. This will be removed soon.
type Config struct {
	Kernel    string // Kernel for Utility VM (embedded in a UEFI bootloader)
	Initrd    string // Initrd image for Utility VM
	Utilities string // VHD containing the utilities for the service VM in the case of Kernel+Initrd
	Vhdx      string // VHD for booting the utility VM
	Name      string // Name of the utility VM
	Svm       bool   // Is a service VM
}

// DefaultConfig generates a default config from a set of options
// If baseDir is not supplied, defaults to $env:ProgramFiles\lcow
func DefaultConfig(baseDir string, options []string) (Config, error) {
	if baseDir == "" {
		baseDir = filepath.Join(os.Getenv("ProgramFiles"), "lcow")
	}

	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return Config{}, fmt.Errorf("opengcs: cannot create default utility VM configuration as directory '%s' was not found", baseDir)
	}

	config := Config{
		Vhdx:      filepath.Join(baseDir, `uvm.vhdx`),
		Kernel:    filepath.Join(baseDir, `bootx64.efi`),
		Initrd:    filepath.Join(baseDir, `initrd.img`),
		Utilities: filepath.Join(baseDir, `sandbox.vhdx`),
		Svm:       false,
	}

	// TODO: @jhowardmsft: Platform change in-flight. Utliities can be removed.
	for _, v := range options {
		opt := strings.SplitN(v, "=", 2)
		if len(opt) == 2 {
			switch strings.ToLower(opt[0]) {
			case "lcowuvmkernel":
				config.Kernel = opt[1]
			case "lcowuvminitrd":
				config.Initrd = opt[1]
			case "lcowuvmmutilities":
				config.Utilities = opt[1]
			case "lcowuvmvhdx":
				config.Vhdx = opt[1]
			}
		}
	}

	return config, nil
}

// Validate validates a Config structure for starting a utility VM.
func (config *Config) Validate() (Mode, []string, error) {
	var warnings []string

	// Validate that if VHDX requested, it exists.
	if config.Vhdx != "" {
		if _, err := os.Stat(config.Vhdx); os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("opengcs: vhdx for utility VM boot '%s' could not be found", config.Vhdx))
		} else {
			return ModeVhdx, warnings, nil
		}
	}

	// So must be kernel+initrd
	if config.Initrd == "" && config.Kernel == "" {
		return ModeError, warnings, fmt.Errorf("opengcs: both initrd and kernel options for utility VM boot must be supplied")
	}
	if _, err := os.Stat(config.Kernel); os.IsNotExist(err) {
		return ModeError, warnings, fmt.Errorf("opengcs: kernel '%s' was not found", config.Kernel)
	}
	if _, err := os.Stat(config.Initrd); os.IsNotExist(err) {
		return ModeError, warnings, fmt.Errorf("opengcs: initrd '%s' was not found", config.Initrd)
	}
	dk, _ := filepath.Split(config.Kernel)
	di, _ := filepath.Split(config.Initrd)
	if dk != di {
		return ModeError, warnings, fmt.Errorf("initrd '%s' and kernel '%s' must be located in the same directory")
	}

	if config.Utilities == "" {
		return ModeError, warnings, fmt.Errorf("utilities must be supplied for kernel and initrd utility VM boot")
	}
	if _, err := os.Stat(config.Utilities); os.IsNotExist(err) {
		return ModeError, warnings, fmt.Errorf("utilities '%s' was not found", config.Utilities)
	}

	return ModeKernelInitrd, warnings, nil
}

// Create creates a utility VM from a configuration.
func (config *Config) Create() (hcsshim.Container, error) {
	logrus.Debugf("opengcs Create: %+v", config)

	mode, _, err := config.Validate()
	if err != nil {
		return nil, err
	}

	mvds := []hcsshim.MappedVirtualDisk{}
	mvds = append(mvds, hcsshim.MappedVirtualDisk{
		HostPath:          config.Utilities,
		ContainerPath:     fmt.Sprintf("/mnt/gcs/%s/scratch", config.Name), // TODO @jhowardmsft, platform change in-flight. This will move to /mnt/servicevm/utilities imminently
		ReadOnly:          true,
		CreateInUtilityVM: true,
	})

	configuration := &hcsshim.ContainerConfig{
		HvPartition:                 true,
		Name:                        config.Name,
		SystemType:                  "container",
		ContainerType:               "linux",
		Servicing:                   config.Svm, // TODO @jhowardmsft Need to stop overloading this field but needs platform change that is in-flight
		TerminateOnLastHandleClosed: true,
		// @jhowardmsft platform change in-flight. Add this next line and remove setting of LayerFolderPath
		//MappedVirtualDisks:          mvds, // TODO @jhowardmsft - see comment above about mvds and change in-flight.
	}

	dir, _ := filepath.Split(config.Utilities)
	configuration.LayerFolderPath = dir

	if mode == ModeVhdx {
		configuration.HvRuntime = &hcsshim.HvRuntime{
			ImagePath: config.Vhdx,
		}
	} else {
		//dir, _ := filepath.Split(config.Initrd)
		// TODO @jhowardmsft - with a platform change that is in-flight, remove ImagePath for
		// initrd/kernel boot. Current platform requires it.
		configuration.HvRuntime = &hcsshim.HvRuntime{
			ImagePath:       dir,
			LinuxInitrdPath: config.Initrd,
			LinuxKernelPath: config.Kernel,
		}
	}

	configurationS, _ := json.Marshal(configuration)
	logrus.Debugf("opengcs Create: Calling HCS with '%s'", string(configurationS))
	uvm, err := hcsshim.CreateContainer(config.Name, configuration)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("opengcs Create: uvm created, starting...")
	err = uvm.Start()
	if err != nil {
		logrus.Debugf("opengcs Create: uvm failed to start: %s", err)
		// Make sure we don't leave it laying around as it's been created in HCS
		uvm.Terminate()
		return nil, err
	}

	logrus.Debugf("opengcs Create: uvm %s is running", config.Name)
	return uvm, nil
}
