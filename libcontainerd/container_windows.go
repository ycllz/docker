package libcontainerd

import (
	"syscall"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/restartmanager"
)

type container struct {
	process
	restartManager restartmanager.RestartManager
	restarting     bool
	processes      map[string]*process
}

func (c *container) start(spec *Spec) error {
	var err error

	// Start the container
	logrus.Debugln("Starting container ", c.id)
	if err = hcsshim.StartComputeSystem(c.id); err != nil {
		logrus.Errorf("Failed to start compute system: %s", err)
		return err
	}

	defer func() {
		// Stop the container
		logrus.Debugf("Shutting down container %s", c.id)
		if err := hcsshim.ShutdownComputeSystem(c.id, hcsshim.TimeoutInfinite, "lcd-contwin-start-defer"); err != nil {
			if herr, ok := err.(*hcsshim.HcsError); !ok ||
				(herr.Err != hcsshim.ERROR_SHUTDOWN_IN_PROGRESS &&
					herr.Err != ErrorBadPathname &&
					herr.Err != syscall.ERROR_PATH_NOT_FOUND) {
				logrus.Warnf("Ignoring error from ShutdownComputeSystem %s", err)
			}
		}
	}()

	createProcessParms := hcsshim.CreateProcessParams{
		EmulateConsole:   spec.Process.Terminal,
		WorkingDirectory: spec.Process.Cwd,
		ConsoleSize:      spec.Process.InitialConsoleSize,
	}

	// Configure the environment for the process
	createProcessParms.Environment = setupEnvironmentVariables(spec.Process.Env)
	if createProcessParms.CommandLine, err = createCommandLine(spec); err != nil {
		return err
	}

	// Start the command running in the container.
	pid, _, _, _, err := hcsshim.CreateProcessInComputeSystem(c.id, false, false, false, createProcessParms)
	// HACK HACK ^^
	//pid, stdin, stdout, stderr, err := hcsshim.CreateProcessInComputeSystem(c.id, pipes.Stdin != nil, true, !spec.Process.Terminal, createProcessParms)
	if err != nil {
		logrus.Errorf("CreateProcessInComputeSystem() failed %s", err)
		return err
	}

	// Now that the process has been launched, begin copying data to and from
	// the named pipes for the std handles.
	// HACK HACK setupPipes(stdin, stdout, stderr, pipes)

	//Save the PID as we'll need this in Kill()
	logrus.Debugf("PID %d", pid)
	c.systemPid = uint32(pid)

	/// FROM ORIGINAL EXEC DRIVER

	//	if spec.Process.Terminal {
	//		term = NewTtyConsole(id, pid)
	//	} else {
	//		term = NewStdConsole()
	//	}
	//	c.ProcessConfig.Terminal = term

	//	//	// Maintain our list of active containers. We'll need this later for exec
	//	//	// and other commands.
	//	//	d.Lock()
	//	//	d.activeContainers[id] = &activeContainer{
	//	//		command: c,
	//	//	}
	//	//	d.Unlock()

	//exitCode, err := hcsshim.WaitForProcessInComputeSystem(c.id, pid, hcsshim.TimeoutInfinite)
	// HACK HACK NEXT LINE (exitCode)
	_, err = hcsshim.WaitForProcessInComputeSystem(c.id, pid, hcsshim.TimeoutInfinite)
	if err != nil {
		if herr, ok := err.(*hcsshim.HcsError); ok && herr.Err != syscall.ERROR_BROKEN_PIPE {
			logrus.Warnf("WaitForProcessInComputeSystem failed (container may have been killed): %s", err)
		}
		// Do NOT return err here as the container would have
		// started, otherwise docker will deadlock. It's perfectly legitimate
		// for WaitForProcessInComputeSystem to fail in situations such
		// as the container being killed on another thread.

		// HACKHACK BUGBUG What to do here???????
		//return execdriver.ExitStatus{ExitCode: hcsshim.WaitErrExecFailed}, nil
	}

	/// LINUX BELOW

	//	iopipe, err := c.openFifos()
	//	if err != nil {
	//		return err
	//	}

	//	r := &containerd.CreateContainerRequest{
	//		Id:         c.id,
	//		BundlePath: c.dir,
	//		Stdin:      c.fifo(syscall.Stdin),
	//		Stdout:     c.fifo(syscall.Stdout),
	//		Stderr:     c.fifo(syscall.Stderr),
	//	}

	c.client.appendContainer(c)

	//	resp, err := c.client.remote.apiClient.CreateContainer(context.Background(), r)
	//	if err != nil {
	//		return err
	//	}

	//	// FIXME: is there a race for closing stdin before container starts
	//	if err := c.client.backend.AttachStreams(c.id, *iopipe); err != nil {
	//		return err
	//	}

	return c.client.backend.StateChanged(c.id, StateInfo{
		State: StateStart,
		Pid:   c.systemPid,
	})

}

//		// HACK HACK BUGBUG - This was passed in to the old exec driver
//	var pipes *Pipes

//	var (
//		term Terminal
//	)
