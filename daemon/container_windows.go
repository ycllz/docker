package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/daemon/graphdriver/windows"
	"github.com/docker/docker/engine"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/hcsshim"
)

func (container *Container) AllocateNetwork() error {
	mode := container.hostConfig.NetworkMode
	if container.Config.NetworkDisabled || !mode.IsPrivate() {
		return nil
	}

	var (
		env *engine.Env
		err error
		eng = container.daemon.eng
	)

	job := eng.Job("allocate_interface", container.ID)
	job.Setenv("RequestedMac", container.Config.MacAddress)
	if env, err = job.Stdout.AddEnv(); err != nil {
		return err
	}
	if err = job.Run(); err != nil {
		return err
	}

	container.NetworkSettings.Bridge = env.Get("Bridge")
	container.NetworkSettings.MacAddress = env.Get("MacAddress")

	return nil
}

func (container *Container) Kill() error {
	if !container.IsRunning() {
		return nil
	}

	// 1. Send SIGKILL
	if err := container.killPossiblyDeadProcess(9); err != nil {
		return err
	}

	// 2. Wait for the process to die, in last resort, try to kill the process directly
	// TODO Windows: Equivalent to Linux

	container.WaitStop(-1 * time.Second)
	return nil
}

// TODO WINDOWS - This function still needs more refactoring.
func (container *Container) Start() (err error) {
	container.Lock()
	defer container.Unlock()

	if container.Running {
		return nil
	}

	// if we encounter an error during start we need to ensure that any other
	// setup has been cleaned up properly
	defer func() {
		if err != nil {
			container.setError(err)
			// if no one else has set it, make sure we don't leave it at zero
			if container.ExitCode == 0 {
				container.ExitCode = 128
			}
			container.toDisk()
			container.cleanup()
		}
	}()

	if err := container.Mount(); err != nil {
		return err
	}

	// Once the container filesystem is mounted, prepare the layer to
	// boot using the Windows driver.
	if err := container.PrepareStorage(); err != nil {
		return err
	}

	if err := container.initializeNetworking(); err != nil {
		return err
	}

	// This is where is Linux it calls into the Exec Driver. TODO WINDOWS (see populateCommand())
	if err := populateCommand(container, nil); err != nil {
		return err
	}

	return container.waitForStart()
}

// TODO WINDOWS
// This can be totally factored out but currently also used in create.go
func (container *Container) prepareVolumes() error {
	return nil
}

func (container *Container) initializeNetworking() error {
	if container.daemon.config.DisableNetwork {
		container.Config.NetworkDisabled = true
		return nil
	}
	if err := container.AllocateNetwork(); err != nil {
		return err
	}
	return nil
}

// TODO WINDOWS
// GetSize, return real size, virtual size
func (container *Container) GetSize() (int64, int64) {
	return 0, 0
}

func (container *Container) ExportRw() (archive.Archive, error) {
	if container.IsRunning() {
		return nil, fmt.Errorf("Cannot export a running container.")
	}
	return nil, nil
}

func populateCommand(c *Container, env []string) error {
	en := &execdriver.Network{
		Mtu:       c.daemon.config.Mtu,
		Interface: nil,
	}

	parts := strings.SplitN(string(c.hostConfig.NetworkMode), ":", 2)
	switch parts[0] {
	case "none":
	case "bridge", "": // empty string to support existing containers
		if !c.Config.NetworkDisabled {
			network := c.NetworkSettings
			en.Interface = &execdriver.NetworkInterface{
				Bridge:     network.Bridge,
				MacAddress: network.MacAddress,
			}
		}
	case "host", "container":
		return fmt.Errorf("unsupported network mode: %s", c.hostConfig.NetworkMode)
	default:
		return fmt.Errorf("invalid network mode: %s", c.hostConfig.NetworkMode)
	}

	pid := &execdriver.Pid{}
	pid.HostPid = c.hostConfig.PidMode.IsHost()

	resources := &execdriver.Resources{
		Memory:     c.hostConfig.Memory,
		MemorySwap: c.hostConfig.MemorySwap,
		CpuShares:  c.hostConfig.CpuShares,
		CpusetCpus: c.hostConfig.CpusetCpus,
	}

	processConfig := execdriver.ProcessConfig{
		Privileged: c.hostConfig.Privileged,
		Entrypoint: c.Path,
		Arguments:  c.Args,
		Tty:        c.Config.Tty,
		User:       c.Config.User,
	}

	processConfig.Env = env

	c.command = &execdriver.Command{
		ID:             c.ID,
		Rootfs:         c.RootfsPath(),
		ReadonlyRootfs: c.hostConfig.ReadonlyRootfs,
		InitPath:       "/.dockerinit",
		WorkingDir:     c.Config.WorkingDir,
		Network:        en,
		//		Ipc:                ipc,
		Pid:       pid,
		Resources: resources,
		//		AllowedDevices:     allowedDevices,
		//		AutoCreatedDevices: autoCreatedDevices,
		CapAdd:        c.hostConfig.CapAdd,
		CapDrop:       c.hostConfig.CapDrop,
		ProcessConfig: processConfig,
		ProcessLabel:  c.GetProcessLabel(),
		MountLabel:    c.GetMountLabel(),
		//		LxcConfig:          lxcConfig,
		//		AppArmorProfile:    c.AppArmorProfile,
		Dummy: c.Config.Dummy,
	}

	return nil
}

func (container *Container) PrepareStorage() error {
	if wd, ok := container.daemon.driver.(*windows.WindowsGraphDriver); ok {
		// Get list of paths to parent layers.
		var ids []string
		if container.ImageID != "" {
			img, err := container.daemon.graph.Get(container.ImageID)
			if err != nil {
				return err
			}

			ids, err = container.daemon.graph.ParentLayerIds(img)
			if err != nil {
				return err
			}
		}

		if err := hcsshim.PrepareLayer(wd.Info(), container.ID, wd.LayerIdsToPaths(ids)); err != nil {
			return err
		}
	}
	return nil
}

func (container *Container) CleanupStorage() error {
	if wd, ok := container.daemon.driver.(*windows.WindowsGraphDriver); ok {
		return hcsshim.UnprepareLayer(wd.Info(), container.ID)
	}
	return nil
}
