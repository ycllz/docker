// +build daemon

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/cli"
	"github.com/docker/docker/cliconfig"
	"github.com/docker/docker/daemon"
	flag "github.com/docker/docker/pkg/mflag"
	"github.com/docker/docker/pkg/system"
)

const daemonUsage = "       docker daemon [ --help | ... ]\n"

var (
	flDaemon              = flag.Bool([]string{"#d", "#-daemon"}, false, "Enable daemon mode (deprecated; use docker daemon)")
	daemonCli cli.Handler = NewDaemonCli()
)

// TODO: remove once `-d` is retired
func handleGlobalDaemonFlag() {
	// This block makes sure that if the deprecated daemon flag `--daemon` is absent,
	// then all daemon-specific flags are absent as well.
	if !*flDaemon && daemonFlags != nil {
		flag.CommandLine.Visit(func(fl *flag.Flag) {
			for _, name := range fl.Names {
				name := strings.TrimPrefix(name, "#")
				if daemonFlags.Lookup(name) != nil {
					// daemon flag was NOT specified, but daemon-specific flags were
					// so let's error out
					fmt.Fprintf(os.Stderr, "docker: the daemon flag '-%s' must follow the 'docker daemon' command.\n", name)
					os.Exit(1)
				}
			}
		})
	}

	if *flDaemon {
		daemonCli.(*DaemonCli).CmdDaemon(flag.Args()...)
		os.Exit(0)
	}
}

func presentInHelp(usage string) string { return usage }
func absentFromHelp(string) string      { return "" }

// NewDaemonCli returns a pre-configured daemon CLI
func NewDaemonCli() *DaemonCli {
	daemonFlags = cli.Subcmd("daemon", nil, "Enable daemon mode", true)

	// TODO(tiborvass): remove InstallFlags?
	daemonConfig := new(daemon.Config)
	daemonConfig.LogConfig.Config = make(map[string]string)
	daemonConfig.ClusterOpts = make(map[string]string)
	daemonConfig.InstallFlags(daemonFlags, presentInHelp)
	daemonConfig.InstallFlags(flag.CommandLine, absentFromHelp)
	daemonFlags.Require(flag.Exact, 0)

	return &DaemonCli{
		Config: daemonConfig,
	}
}

func migrateKey() (err error) {
	// Migrate trust key if exists at ~/.docker/key.json and owned by current user
	oldPath := filepath.Join(cliconfig.ConfigDir(), defaultTrustKeyFile)
	newPath := filepath.Join(getDaemonConfDir(), defaultTrustKeyFile)
	if _, statErr := os.Stat(newPath); os.IsNotExist(statErr) && currentUserIsOwner(oldPath) {
		defer func() {
			// Ensure old path is removed if no error occurred
			if err == nil {
				err = os.Remove(oldPath)
			} else {
				logrus.Warnf("Key migration failed, key file not removed at %s", oldPath)
				os.Remove(newPath)
			}
		}()

		if err := system.MkdirAll(getDaemonConfDir(), os.FileMode(0644)); err != nil {
			return fmt.Errorf("Unable to create daemon configuration directory: %s", err)
		}

		newFile, err := os.OpenFile(newPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("error creating key file %q: %s", newPath, err)
		}
		defer newFile.Close()

		oldFile, err := os.Open(oldPath)
		if err != nil {
			return fmt.Errorf("error opening key file %q: %s", oldPath, err)
		}
		defer oldFile.Close()

		if _, err := io.Copy(newFile, oldFile); err != nil {
			return fmt.Errorf("error copying key: %s", err)
		}

		logrus.Infof("Migrated key from %s to %s", oldPath, newPath)
	}

	return nil
}

// DaemonCli represents the daemon CLI.
type DaemonCli struct {
	*daemon.Config
}

func getGlobalFlag() (globalFlag *flag.Flag) {
	defer func() {
		if x := recover(); x != nil {
			switch f := x.(type) {
			case *flag.Flag:
				globalFlag = f
			default:
				panic(x)
			}
		}
	}()
	visitor := func(f *flag.Flag) { panic(f) }
	commonFlags.FlagSet.Visit(visitor)
	clientFlags.FlagSet.Visit(visitor)
	return
}

// CmdDaemon is the daemon command, called the raw arguments after `docker daemon`.
func (cli *DaemonCli) CmdDaemon(args ...string) error {
	return nil
}

// shutdownDaemon just wraps daemon.Shutdown() to handle a timeout in case
// d.Shutdown() is waiting too long to kill container or worst it's
// blocked there
func shutdownDaemon(d *daemon.Daemon, timeout time.Duration) {
	ch := make(chan struct{})
	go func() {
		d.Shutdown()
		close(ch)
	}()
	select {
	case <-ch:
		logrus.Debug("Clean shutdown succeeded")
	case <-time.After(timeout * time.Second):
		logrus.Error("Force shutdown daemon")
	}
}
