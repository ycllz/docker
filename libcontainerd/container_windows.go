package libcontainerd

import (
	"io"
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

func (c *container) start() error {
	var err error

	// Start the container
	logrus.Debugln("Starting container ", c.id)
	if err = hcsshim.StartComputeSystem(c.id); err != nil {
		logrus.Errorf("Failed to start compute system: %s", err)
		return err
	}

	//	defer func() {
	//		// Stop the container
	//		logrus.Debugf("Shutting down container %s", c.id)
	//		if err := hcsshim.ShutdownComputeSystem(c.id, hcsshim.TimeoutInfinite, "lcd-contwin-start-defer"); err != nil {
	//			if herr, ok := err.(*hcsshim.HcsError); !ok ||
	//				(herr.Err != hcsshim.ERROR_SHUTDOWN_IN_PROGRESS &&
	//					herr.Err != ErrorBadPathname &&
	//					herr.Err != syscall.ERROR_PATH_NOT_FOUND) {
	//				logrus.Warnf("Ignoring error from ShutdownComputeSystem %s", err)
	//			}
	//		}
	//	}()

	createProcessParms := hcsshim.CreateProcessParams{
		EmulateConsole:   c.process.ociProcess.Terminal,
		WorkingDirectory: c.process.ociProcess.Cwd,
		ConsoleSize:      c.process.ociProcess.InitialConsoleSize,
	}

	// Configure the environment for the process
	createProcessParms.Environment = setupEnvironmentVariables(c.process.ociProcess.Env)

	// HACK HACK
	createProcessParms.CommandLine = "cmd /s /c tasklist"
	//	if createProcessParms.CommandLine, err = createCommandLine(spec); err != nil {
	//		return err
	//	}

	iopipe := &IOPipe{Terminal: c.process.ociProcess.Terminal}

	var (
		pid            uint32
		stdout, stderr io.ReadCloser
	)

	// Start the command running in the container.
	pid, iopipe.Stdin, stdout, stderr, err = hcsshim.CreateProcessInComputeSystem(
		c.id, true, true, true, createProcessParms)
	if err != nil {
		logrus.Errorf("CreateProcessInComputeSystem() failed %s", err)
		return err
	}

	// Convert io.ReadClosers to io.Readers
	iopipe.Stdout = openReaderFromPipe(stdout)
	iopipe.Stderr = openReaderFromPipe(stderr)

	//Save the PID as we'll need this in Kill()
	logrus.Debugf("Process started - PID %d", pid)
	c.systemPid = uint32(pid)

	// Spin up a go routine waiting for exit to handle cleanup
	go c.waitExit(pid, true)

	/// FROM ORIGINAL EXEC DRIVER

	//exitCode, err := hcsshim.WaitForProcessInComputeSystem(c.id, pid, hcsshim.TimeoutInfinite)
	// HACK HACK NEXT LINE (exitCode)
	//	_, err = hcsshim.WaitForProcessInComputeSystem(c.id, pid, hcsshim.TimeoutInfinite)
	//	if err != nil {
	//		if herr, ok := err.(*hcsshim.HcsError); ok && herr.Err != syscall.ERROR_BROKEN_PIPE {
	//			logrus.Warnf("WaitForProcessInComputeSystem failed (container may have been killed): %s", err)
	//		}
	// Do NOT return err here as the container would have
	// started, otherwise docker will deadlock. It's perfectly legitimate
	// for WaitForProcessInComputeSystem to fail in situations such
	// as the container being killed on another thread.

	// HACKHACK BUGBUG What to do here???????
	//return execdriver.ExitStatus{ExitCode: hcsshim.WaitErrExecFailed}, nil
	//}

	c.client.appendContainer(c)

	// FIXME: is there a race for closing stdin before container starts
	if err := c.client.backend.AttachStreams(c.id, *iopipe); err != nil {
		return err
	}

	return c.client.backend.StateChanged(c.id, StateInfo{
		State: StateStart,
		Pid:   c.systemPid,
	})

}

// waitExit runs as a goroutine waiting for the process to exit. It's
// equivalent to (in the linux containerd world) where events come in for
// state change notifications from containerd.
func (c *container) waitExit(pid uint32, isInitProcess bool) error {
	logrus.Debugln("waitExit on ", pid)

	// If this is the init process, always call into vmcompute.dll to
	// shutdown the container after we have completed.
	if isInitProcess {
		defer func() {
			// However it is the init process, so shutdown the container
			logrus.Debugf("Shutting down container %s", c.id)
			if err := hcsshim.ShutdownComputeSystem(c.id, hcsshim.TimeoutInfinite, "waitExit"); err != nil {
				if herr, ok := err.(*hcsshim.HcsError); !ok ||
					(herr.Err != hcsshim.ERROR_SHUTDOWN_IN_PROGRESS &&
						herr.Err != ErrorBadPathname &&
						herr.Err != syscall.ERROR_PATH_NOT_FOUND) {
					logrus.Warnf("Ignoring error from ShutdownComputeSystem %s", err)
				}
			} else {
				logrus.Debugf("Completed shutting down container %s", c.id)
			}
		}()
	}

	// Block indefinitely for the process to exit.
	exitCode, err := hcsshim.WaitForProcessInComputeSystem(c.id, pid, hcsshim.TimeoutInfinite)
	if err != nil {
		if herr, ok := err.(*hcsshim.HcsError); ok && herr.Err != syscall.ERROR_BROKEN_PIPE {
			logrus.Warnf("WaitForProcessInComputeSystem failed (container may have been killed): %s", err)
		}
		return nil
	}

	// Assume the container has exited
	st := StateInfo{
		State:    StateExit,
		ExitCode: uint32(exitCode),
	}

	// It could be an exec'd process which exited
	if !isInitProcess {
		st.State = StateExitProcess
	}

	c.client.lock(c.id)
	defer c.client.unlock(c.id)

	if st.State == StateExit && c.restartManager != nil {
		restart, wait, err := c.restartManager.ShouldRestart(uint32(exitCode))
		if err != nil {
			logrus.Error(err)
		} else if restart {
			st.State = StateRestart
			c.restarting = true
			go func() {
				err := <-wait
				c.restarting = false
				if err != nil {
					logrus.Error(err)
				} else {
					c.start()
				}
			}()
		}
	}

	// Remove process from list if we have exited
	// We need to do so here in case the Message Handler decides to restart it.
	c.client.deleteContainer(c.processID)

	// Call into the backend to notify it of the state change.
	logrus.Debugln("waitExit() calling backend.StateChanged %v", st)
	if err := c.client.backend.StateChanged(c.id, st); err != nil {
		logrus.Error(err)
	}

	logrus.Debugln("waitExit() completed OK")
	return nil
}
