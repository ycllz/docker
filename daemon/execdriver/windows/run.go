// +build windows

package windows

// Note this is alpha code for the bring up of containers on Windows.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/pkg/hcsshim"
	"github.com/natefinch/npipe"
)

func (d *driver) Run(c *execdriver.Command, pipes *execdriver.Pipes, startCallback execdriver.StartCallback) (execdriver.ExitStatus, error) {

	var (
		term                           execdriver.Terminal
		err                            error
		inListen, outListen, errListen *npipe.PipeListener
	)

	// Make sure the client isn't asking for options which aren't supported
	err = checkSupportedOptions(c)
	if err != nil {
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	type defConfig struct {
		DefFile string
	}

	type networkConnection struct {
		NetworkName string
		EnableNat   bool
	}
	type networkSettings struct {
		MacAddress string
	}

	type device struct {
		DeviceType string
		Connection interface{}
		Settings   interface{}
	}

	type containerInit struct {
		SystemType  string
		Name        string
		IsDummy     bool
		VolumePath  string
		Definitions []defConfig
		Devices     []device
	}

	// TODO Windows. This could possibly be an exec option. Note this is a
	// daemon development variable only and should not be used for running
	// production containers on Windows
	execDummyMode := false
	if len(os.Getenv("windowsexecdummy")) > 0 {
		logrus.Warn("Using dummy mode in Windows exec driver Run(). This is for development use only!")
		execDummyMode = true
	}

	cu := &containerInit{
		SystemType: "Container",
		Name:       c.ID,
		IsDummy:    execDummyMode,
		VolumePath: c.Rootfs,
	}

	// HCS will balk if  definitions are configured in dummy mode
	if !execDummyMode {
		cu.Definitions = []defConfig{{fmt.Sprintf(`%s\container.def`, c.Rootfs)}}
	}

	if c.Network.Interface != nil {
		dev := device{
			DeviceType: "Network",
			Connection: &networkConnection{
				NetworkName: c.Network.Interface.Bridge,
				EnableNat:   false,
			},
		}

		if c.Network.Interface.MacAddress != "" {
			windowsStyleMAC := strings.Replace(
				c.Network.Interface.MacAddress, ":", "-", -1)
			dev.Settings = networkSettings{
				MacAddress: windowsStyleMAC,
			}
		}

		cu.Devices = append(cu.Devices, dev)
	}

	configurationb, err := json.Marshal(cu)
	if err != nil {
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	configuration := string(configurationb)

	err = hcsshim.CreateComputeSystem(c.ID, configuration)
	if err != nil {
		logrus.Debugln("Failed to create temporary container ", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	// Start the container
	logrus.Debugln("Starting container ", c.ID)
	err = hcsshim.StartComputeSystem(c.ID)
	if err != nil {
		logrus.Errorf("Failed to start compute system: %s", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}
	defer func() {
		// Stop the container
		logrus.Debugf("Shutting down container %s", c.ID)
		if err := hcsshim.ShutdownComputeSystem(c.ID); err != nil {
			// IMPORTANT: Don't fail if fails to change state. It could already
			// have been stopped through kill().
			// Otherwise, the docker daemon will hang in job wait()
			logrus.Warnf("Ignoring error from ShutdownComputeSystem %s", err)
		}
	}()

	// We use a different pipe name between real and dummy mode in the HCS
	var pipePrefix string
	if execDummyMode {
		pipePrefix = `\\.\pipe\`
	} else {
		pipePrefix = fmt.Sprintf(`\\.\Containers\%s\Device\NamedPipe\`, c.ID)
	}

	// This is what gets passed into the exec - structure containing the
	// names of the named pipes for stdin/out/err
	stdDevices := hcsshim.Devices{}

	// Connect stdin
	if pipes.Stdin != nil {
		stdDevices.StdInPipe = pipePrefix + c.ID + "-stdin"

		// Listen on the named pipe
		inListen, err = npipe.Listen(stdDevices.StdInPipe)
		if err != nil {
			logrus.Errorf("stdin failed to listen on %s err=%s", stdDevices.StdInPipe, err)
			return execdriver.ExitStatus{ExitCode: -1}, err
		}
		defer inListen.Close()

		// Launch a goroutine to do the accept. We do this so that we can
		// cause an otherwise blocking goroutine to gracefully close when
		// the caller (us) closes the listener
		go stdinAccept(inListen, stdDevices.StdInPipe, pipes.Stdin)
	}

	// Connect stdout
	stdDevices.StdOutPipe = pipePrefix + c.ID + "-stdout"
	outListen, err = npipe.Listen(stdDevices.StdOutPipe)
	if err != nil {
		logrus.Errorf("stdout failed to listen on %s err=%s", stdDevices.StdOutPipe, err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}
	defer outListen.Close()
	go stdouterrAccept(outListen, stdDevices.StdOutPipe, pipes.Stdout)

	// No stderr on TTY.
	if !c.ProcessConfig.Tty {
		// Connect stderr
		stdDevices.StdErrPipe = pipePrefix + c.ID + "-stderr"
		errListen, err = npipe.Listen(stdDevices.StdErrPipe)
		if err != nil {
			logrus.Errorf("stderr failed to listen on %s err=%s", stdDevices.StdErrPipe, err)
			return execdriver.ExitStatus{ExitCode: -1}, err
		}
		defer errListen.Close()
		go stdouterrAccept(errListen, stdDevices.StdErrPipe, pipes.Stderr)
	}

	if c.ProcessConfig.Tty {
		term = NewTtyConsole()
	} else {
		term = NewStdConsole()
	}
	c.ProcessConfig.Terminal = term

	// This should get caught earlier, but just in case - validate that we
	// have something to run
	if c.ProcessConfig.Entrypoint == "" {
		err = errors.New("No entrypoint specified")
		logrus.Error(err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	// Build the command line of the process
	commandLine := c.ProcessConfig.Entrypoint
	for _, arg := range c.ProcessConfig.Arguments {
		logrus.Debugln("appending ", arg)
		commandLine += " " + arg
	}
	logrus.Debugf("commandLine: %s", commandLine)

	// TTY or not is passed into executing a process. Declared as uint32 to
	// match the Win32 definition
	var emulateTTY uint32 = 0x00000000
	if c.ProcessConfig.Tty == true {
		emulateTTY = 0x00000001
	}

	// Start the command running in the container.
	var pid uint32
	pid, err = hcsshim.CreateProcessInComputeSystem(c.ID,
		"", // This will be applicationname
		commandLine,
		c.WorkingDir,
		stdDevices,
		emulateTTY)
	if err != nil {
		logrus.Errorf("CreateProcessInComputeSystem() failed %s", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	//Save the PID as we'll need this in Kill()
	logrus.Debugln("PID ", pid)
	c.ContainerPid = int(pid)

	// Maintain our list of active containers. We'll need this later for exec
	// and other commands.
	d.Lock()
	d.activeContainers[c.ID] = &activeContainer{
		command: c,
	}
	d.Unlock()

	// Invoke the start callback
	if startCallback != nil {
		startCallback(&c.ProcessConfig, int(pid))
	}

	var exitCode uint32
	exitCode, err = hcsshim.WaitForProcessInComputeSystem(c.ID, pid)
	if err != nil {
		logrus.Errorf("Failed to WaitForProcessInComputeSystem %s", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	// TODO - Do something with this exit code (!)
	logrus.Debugf("exitcode=%d err=%s", exitCode, err)

	logrus.Debugf("Exiting Run() with ExitCode 0 id=%s", c.ID)
	return execdriver.ExitStatus{ExitCode: 0}, nil
}
