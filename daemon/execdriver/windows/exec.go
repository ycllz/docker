// +build windows

package windows

import (
	"errors"
	"fmt"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/pkg/hcsshim"
	"github.com/docker/docker/pkg/stringid"
	"github.com/natefinch/npipe"
)

func (d *driver) Exec(c *execdriver.Command, processConfig *execdriver.ProcessConfig, pipes *execdriver.Pipes, startCallback execdriver.StartCallback) (int, error) {

	var inListen, outListen, errListen *npipe.PipeListener

	active := d.activeContainers[c.ID]
	if active == nil {
		return -1, fmt.Errorf("Exec - No active container exists with ID %s", c.ID)
	}

	var (
		term execdriver.Terminal
		err  error
	)

	// We use another unique ID here for each exec instance otherwise it
	// may conflict with the pipe name being used by RUN.

	// We use a different pipe name between real and dummy mode in the HCS
	var pipePrefix string
	var randomID string = stringid.GenerateRandomID()

	if dummyMode {
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
			logrus.Errorf("stdin failed to listen on %s %s ", stdDevices.StdInPipe, err)
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
		logrus.Errorf("stdout failed to listen on %s %s", stdDevices.StdOutPipe, err)
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
			logrus.Errorf("Stderr failed to listen on %s %s", stdDevices.StdErrPipe, err)
			return -1, err
		}
		defer errListen.Close()
		go stdouterrAccept(errListen, stdDevices.StdErrPipe, pipes.Stderr)
	}

	if c.ProcessConfig.Tty {
		term = NewTtyConsole()
	} else {
		term = NewStdConsole()
	}
	processConfig.Terminal = term

	// While this should get caught earlier, just in case, validate that we
	// have something to run
	if processConfig.Entrypoint == "" {
		err = errors.New("No entrypoint specified")
		logrus.Error(err)
		return -1, err
	}

	// Build the command line of the process
	commandLine := processConfig.Entrypoint
	for _, arg := range processConfig.Arguments {
		logrus.Debugln("appending ", arg)
		commandLine += " " + arg
	}
	logrus.Debugln("commandLine: ", commandLine)

	// TTY or not is passed into executing a process. Note use uint32 to match
	// HCS call
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
		return -1, err
	}

	logrus.Debugln("PID ", pid)

	// Invoke the start callback
	if startCallback != nil {
		startCallback(&c.ProcessConfig, int(pid))
	}

	var exitCode uint32
	if exitCode, err = hcsshim.WaitForProcessInComputeSystem(c.ID, pid); err != nil {
		logrus.Errorf("Failed to WaitForProcessInComputeSystem %s", err)
		return -1, err
	}

	// TODO - Do something with this exit code
	logrus.Debugln("exitcode err", exitCode, err)

	logrus.Debugln("Exiting Run() with ExitCode 0", c.ID)
	return int(exitCode), nil
}
