package daemon

import (
	"fmt"
	"os"
	"syscall"

	"github.com/Sirupsen/logrus"
	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/pkg/signal"
	"github.com/docker/docker/pkg/system"
)

func (d *Daemon) setupDumpStackTrap(root string) {
	// Windows does not support signals like *nix systems. So instead of
	// trapping on SIGUSR1 to dump stacks, we wait on a Win32 event to be
	// signaled.
	go func() {
		sa := syscall.SecurityAttributes{
			Length: 0,
		}
		ev := "Global\\docker-daemon-" + fmt.Sprint(os.Getpid())
		if h, _ := system.CreateEvent(&sa, false, false, ev); h != 0 {
			logrus.Debugf("Stackdump - waiting signal at %s", ev)
			for {
				syscall.WaitForSingleObject(h, syscall.INFINITE)
				signal.DumpStacks(root)
				d.dumpDaemon(root)
			}
		}
	}()
}

// dumpDaemon is a helper function to dump the daemon data-structures
// for debugging purposes.
func (d *Daemon) dumpDaemon(root string) {
	// Ensure we recover from a panic as we are doing this without any locking
	defer func() {
		if r := recover(); r != nil {
		}
	}()
	spew.Dump(d)
}
