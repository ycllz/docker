// +build windows

// This is the Windows driver for containers
package argon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/pkg/common"
	"github.com/docker/docker/pkg/hcsshim"
	"gopkg.in/natefinch/npipe.v2"
)

const (
	DriverName = "Windows. 1854"
	Version    = "30-Mar-2015"
)

var (

	// For device redirection passed into the shim layer.
	stdDevices hcsshim.Devices

	inListen, outListen, errListen *npipe.PipeListener
)

type activeContainer struct {
	command *execdriver.Command
}

type driver struct {
	root             string
	initPath         string
	activeContainers map[string]*activeContainer
	sync.Mutex
}

type info struct {
	ID     string
	driver *driver
}

func NewDriver(root, initPath string) (*driver, error) {
	return &driver{
		root:             root,
		initPath:         initPath,
		activeContainers: make(map[string]*activeContainer),
	}, nil
}

func checkSupportedOptions(c *execdriver.Command) error {
	// Windows doesn't support read-only root filesystem
	if c.ReadonlyRootfs {
		return errors.New("Windows does not support the read-only root filesystem option")
	}

	// Windows doesn't support username
	if c.ProcessConfig.User != "" {
		return errors.New("Windows does not support the username option")
	}

	// Windows doesn't support custom lxc options
	if c.LxcConfig != nil {
		return errors.New("Windows does not support lxc options")
	}

	// Windows doesn't support ulimit
	if c.Resources.Rlimits != nil {
		return errors.New("Windows does not support ulimit options")
	}

	return nil

	// NOTSURE:
	//--add-host=[]              Add a custom host-to-IP mapping (host:ip)
	//-c, --cpu-shares=0         CPU shares (relative weight)
	//--cidfile=                 Write the container ID to the file
	//--cpuset-cpus=             CPUs in which to allow execution (0-3, 0,1)
	//--dns=[]                   Set custom DNS servers
	//--dns-search=[]            Set custom DNS search domains
	//-e, --env=[]               Set environment variables
	//--entrypoint=              Overwrite the default ENTRYPOINT of the image
	//--env-file=[]              Read in a file of environment variables
	//--expose=[]                Expose a port or a range of ports
	//-i, --interactive=false    Keep STDIN open even if not attached
	//-m, --memory=              Memory limit
	//--mac-address=             Container MAC address (e.g. 92:d0:c6:0a:29:33)
	//--memory-swap=             Total memory (memory + swap), '-1' to disable swap
	//--name=                    Assign a name to the container
	//--net=bridge               Set the Network mode for the container
	//-P, --publish-all=false    Publish all exposed ports to random ports
	//-p, --publish=[]           Publish a container's port(s) to the host

	// TODO (Block)
	//--cap-add=[]               Add Linux capabilities
	//--cap-drop=[]              Drop Linux capabilities
	//--device=[]                Add a host device to the container
	//-h, --hostname=            Container host name
	//--ipc=                     IPC namespace to use
	//--link=[]                  Add link to another container
	//DONE --lxc-conf=[]              Add custom lxc options
	//--pid=                     PID namespace to use
	//--privileged=false         Give extended privileges to this container
	//--restart=no               Restart policy to apply when a container exits
	//DONE --read-only=false          Mount the container's root filesystem as read only
	//DONE -u, --user=                Username or UID (format: <name|uid>[:<group|gid>])
	//DONE --ulimit=[]                Ulimit options

	// Allow
	//-d, --detach=false         Run container in background and print container ID

	//--security-opt=[]          Security Options
	//--sig-proxy=true           Proxy received signals to the process
	//-t, --tty=false            Allocate a pseudo-TTY

	//-v, --volume=[]            Bind mount a volume
	//--volumes-from=[]          Mount volumes from the specified container(s)
	//-w, --workdir=             Working directory inside the container

}

func (d *driver) Run(c *execdriver.Command, pipes *execdriver.Pipes, startCallback execdriver.StartCallback) (execdriver.ExitStatus, error) {

	var (
		err error
	)

	// Make sure the client isn't asking for options which aren't supported
	// by Windows containers.
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

	if len(os.Getenv("windowsexecdummy")) > 0 {
		c.Dummy = true
	}

	cu := &containerInit{
		SystemType: "Container",
		Name:       c.ID,
		IsDummy:    c.Dummy,
		VolumePath: c.Rootfs,
	}

	// Dummy mode will balk if the definitions are configured
	if !c.Dummy {
		cu.Definitions = []defConfig{defConfig{fmt.Sprintf(`%s\container.def`, c.Rootfs)}}
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
		log.Debugln("Failed to create temporary container ", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	// Start the container
	log.Debugln("Starting container ", c.ID)
	err = hcsshim.StartComputeSystem(c.ID)
	if err != nil {
		log.Debugln("Failed to start ", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}
	defer func() {
		// Stop the container
		log.Debugln("Shutting down container ", c.ID)
		if err := hcsshim.ShutdownComputeSystem(c.ID); err != nil {
			// IMPORTANT: Don't fail if fails to change state. It could already
			// have been stopped through kill().
			// Otherwise, the docker daemon will hang in job wait()
			log.Debugln("Ignoring error from ShutdownComputeSystem ", err)
		}
	}()

	// We use a different pipe name between real and dummy mode in the HCS
	var pipePrefix string
	if c.Dummy {
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
			log.Debugln("Failed to listen on ", stdDevices.StdInPipe, err)
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
		log.Debugln("Failed to listen on ", stdDevices.StdOutPipe, err)
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
			log.Debugln("Failed to listen on ", stdDevices.StdErrPipe, err)
			return execdriver.ExitStatus{ExitCode: -1}, err
		}
		defer errListen.Close()
		go stdouterrAccept(errListen, stdDevices.StdErrPipe, pipes.Stderr)
	}

	// Sure this would get caught earlier, but just in case - validate that we
	// have something to run
	if c.ProcessConfig.Entrypoint == "" {
		err = errors.New("No entrypoint specified")
		log.Debugln(err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	// Build the command line of the process
	commandLine := c.ProcessConfig.Entrypoint
	for _, arg := range c.ProcessConfig.Arguments {
		log.Debugln("appending ", arg)
		commandLine += " " + arg
	}
	log.Debugln("commandLine: ", commandLine)

	// TTY or not is passed into executing a process
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
		log.Debugln("CreateProcessInComputeSystem() failed ", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	//Save the PID as we'll need this in Kill()
	log.Debugln("PID ", pid)
	c.ContainerPid = int(pid)

	var term execdriver.Terminal
	if c.ProcessConfig.Tty {
		term, err = NewTtyConsole(c.ID, pid)
	} else {
		term, err = NewStdConsole(c.ID)
	}
	c.ProcessConfig.Terminal = term

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
		log.Debugln("Failed to WaitForProcessInComputeSystem ", err)
		return execdriver.ExitStatus{ExitCode: -1}, err
	}

	// TODO - What do we do with this exit code????
	log.Debugln("exitcode err", exitCode, err)

	log.Debugln("Exiting Run() with ExitCode 0", c.ID)
	return execdriver.ExitStatus{ExitCode: 0}, nil
}

func (d *driver) Terminate(p *execdriver.Command) error {
	log.Debugln("WindowsExec: Terminate() ", p.ID)
	return kill(p.ID, p.ContainerPid)
}

func (d *driver) Kill(p *execdriver.Command, sig int) error {
	log.Debugln("WindowsExec: Kill() ", p.ID, sig)
	return kill(p.ID, p.ContainerPid)
}

func kill(ID string, PID int) error {
	log.Debugln("kill() ", ID, PID)

	// Terminate Process
	err := hcsshim.TerminateProcessInComputeSystem(ID, uint32(PID))
	if err != nil {
		log.Debugln("Ignoring error, but failed to terminate process", err)
		// Ignore errors
		err = nil
	}

	// Shutdown the compute system
	err = hcsshim.ShutdownComputeSystem(ID)
	if err != nil {
		log.Debugln("Failed to kill ", ID)
	}

	return err
}

func (d *driver) Pause(c *execdriver.Command) error {
	return fmt.Errorf("Windows: Containers cannot be paused")
}

func (d *driver) Unpause(c *execdriver.Command) error {
	return fmt.Errorf("Windows: Containers cannot be paused")
}

func (i *info) IsRunning() bool {
	var running bool
	running = true // TODO  4/2/15 Asked Lars for an HCS API
	return running
}

func (d *driver) Info(id string) execdriver.Info {
	return &info{
		ID:     id,
		driver: d,
	}
}

func (d *driver) Name() string {
	return fmt.Sprintf("%s Date %s", DriverName, Version)
}

func (d *driver) GetPidsForContainer(id string) ([]int, error) {
	d.Lock()
	//active := d.activeContainers[id]
	d.Unlock()

	// TODO This is wrong, but a start. Need to do this still.
	//var processes []int
	//processes[0] = int(d.activeContainers[id].command.Pid)

	return nil, fmt.Errorf("GetPidsForContainer: GetPidsForContainer() not implemented")
}

func (d *driver) Clean(id string) error {
	return nil
}

func (d *driver) Stats(id string) (*execdriver.ResourceStats, error) {
	return nil, fmt.Errorf("Windows: Stats not implemented")
}

func (d *driver) Exec(c *execdriver.Command, processConfig *execdriver.ProcessConfig, pipes *execdriver.Pipes, startCallback execdriver.StartCallback) (int, error) {

	active := d.activeContainers[c.ID]
	if active == nil {
		return -1, fmt.Errorf("No active container exists with ID %s", c.ID)
	}

	var (
		err error
	)

	// We use another unique ID here for each exec instance otherwise it
	// may conflict with the pipe name being used by RUN.

	// We use a different pipe name between real and dummy mode in the HCS
	var pipePrefix string
	var randomID string = common.GenerateRandomID()

	if c.Dummy {
		pipePrefix = `\\.\pipe\` + randomID + `\`
	} else {
		pipePrefix = fmt.Sprintf(`\\.\Containers\%s\Device\NamedPipe\%s\`, c.ID, randomID)
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
			log.Debugln("Failed to listen on ", stdDevices.StdInPipe, err)
			return -1, err
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
		log.Debugln("Failed to listen on ", stdDevices.StdOutPipe, err)
		return -1, err
	}
	defer outListen.Close()
	go stdouterrAccept(outListen, stdDevices.StdOutPipe, pipes.Stdout)

	// No stderr on TTY.
	if !c.ProcessConfig.Tty {
		// Connect stderr
		stdDevices.StdErrPipe = pipePrefix + c.ID + "-stderr"
		errListen, err = npipe.Listen(stdDevices.StdErrPipe)
		if err != nil {
			log.Debugln("Failed to listen on ", stdDevices.StdErrPipe, err)
			return -1, err
		}
		defer errListen.Close()
		go stdouterrAccept(errListen, stdDevices.StdErrPipe, pipes.Stderr)
	}

	// Sure this would get caught earlier, but just in case - validate that we
	// have something to run
	if processConfig.Entrypoint == "" {
		err = errors.New("No entrypoint specified")
		log.Debugln(err)
		return -1, err
	}

	// Build the command line of the process
	commandLine := processConfig.Entrypoint
	for _, arg := range processConfig.Arguments {
		log.Debugln("appending ", arg)
		commandLine += " " + arg
	}
	log.Debugln("commandLine: ", commandLine)

	// TTY or not is passed into executing a process
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
		log.Debugln("CreateProcessInComputeSystem() failed ", err)
		return -1, err
	}

	var term execdriver.Terminal
	if c.ProcessConfig.Tty {
		term, err = NewTtyConsole(c.ID, pid)
	} else {
		term, err = NewStdConsole(c.ID)
	}
	processConfig.Terminal = term

	log.Debugln("PID ", pid)

	// Invoke the start callback
	if startCallback != nil {
		startCallback(&c.ProcessConfig, int(pid))
	}

	var exitCode uint32
	exitCode, err = hcsshim.WaitForProcessInComputeSystem(c.ID, pid)
	if err != nil {
		log.Debugln("Failed to WaitForProcessInComputeSystem ", err)
		return -1, err
	}

	// TODO - What do we do with this exit code????
	log.Debugln("exitcode err", exitCode, err)

	log.Debugln("Exiting Run() with ExitCode 0", c.ID)
	return int(exitCode), nil
}
