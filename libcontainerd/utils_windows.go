package libcontainerd

import (
	"errors"
	"strings"
	"syscall"

	"github.com/Sirupsen/logrus"
)

// createCommandLine creates a command line from the Entrypoint and args
// of the ProcessConfig. It escapes the arguments if they are not already
// escaped

// HACK HACK BUGBUG - This should be set by caller and entirely in args.

func createCommandLine(spec *Spec) (commandLine string, err error) {
	// While this should get caught earlier, just in case, validate that we
	// have something to run.

	if spec.Process.Entrypoint == "" {
		return "", errors.New("No entrypoint specified")
	}

	// Build the command line of the process
	commandLine = spec.Process.Entrypoint
	logrus.Debugf("Entrypoint: %s", spec.Process.Entrypoint)
	for _, arg := range spec.Process.Args {
		logrus.Debugf("appending %s", arg)
		if !spec.Process.ArgsEscaped {
			arg = syscall.EscapeArg(arg)
		}
		commandLine += " " + arg
	}

	logrus.Debugf("commandLine: %s", commandLine)
	return commandLine, nil
}

// setupEnvironmentVariables convert a string array of environment variables
// into a map as required by the HCS. Source array is in format [v1=k1] [v2=k2] etc.
func setupEnvironmentVariables(a []string) map[string]string {
	r := make(map[string]string)
	for _, s := range a {
		arr := strings.Split(s, "=")
		if len(arr) == 2 {
			r[arr[0]] = arr[1]
		}
	}
	return r
}
