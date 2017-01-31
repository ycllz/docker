// +build linux solaris

package libcontainerd

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	containerd "github.com/docker/containerd/api/grpc/types"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/tonistiigi/fifo"
	"golang.org/x/net/context"
)

type container struct {
	containerCommon

	// Platform specific fields are below here.
	pauseMonitor
	oom         bool
	runtime     string
	runtimeArgs []string
}

type runtime struct {
	path string
	args []string
}

// WithRuntime sets the runtime to be used for the created container
func WithRuntime(path string, args []string) CreateOption {
	return runtime{path, args}
}

func (rt runtime) Apply(p interface{}) error {
	if pr, ok := p.(*container); ok {
		pr.runtime = rt.path
		pr.runtimeArgs = rt.args
	}
	return nil
}

// cleanProcess removes the fifos used by an additional process.
// Caller needs to lock container ID before calling this method.
func (ctr *container) cleanProcess(id string) {
	if p, ok := ctr.processes[id]; ok {
		for _, i := range []int{syscall.Stdin, syscall.Stdout, syscall.Stderr} {
			if err := os.Remove(p.fifo(i)); err != nil && !os.IsNotExist(err) {
				logrus.Warnf("libcontainerd: failed to remove %v for process %v: %v", p.fifo(i), id, err)
			}
		}
	}
	delete(ctr.processes, id)
}

func (ctr *container) spec() (*specs.Spec, error) {
	var spec specs.Spec
	dt, err := ioutil.ReadFile(filepath.Join(ctr.dir, configFilename))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(dt, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (ctr *container) newProcess(friendlyName string) *process {
	return &process{
		dir: ctr.dir,
		processCommon: processCommon{
			containerID:  ctr.containerID,
			friendlyName: friendlyName,
			client:       ctr.client,
		},
	}
}

func (ctr *container) handleEvent(e *containerd.Event) error {
	ctr.client.lock(ctr.containerID)
	defer ctr.client.unlock(ctr.containerID)
	switch e.Type {
	case StateExit, StatePause, StateResume, StateOOM:
		st := StateInfo{
			CommonStateInfo: CommonStateInfo{
				State:    e.Type,
				ExitCode: e.Status,
			},
			OOMKilled: e.Type == StateExit && ctr.oom,
		}
		if e.Type == StateOOM {
			ctr.oom = true
		}
		if e.Type == StateExit && e.Pid != InitFriendlyName {
			st.ProcessID = e.Pid
			st.State = StateExitProcess
		}

		// Remove process from list if we have exited
		switch st.State {
		case StateExit:
			ctr.clean()
			ctr.client.deleteContainer(e.Id)
		case StateExitProcess:
			ctr.cleanProcess(st.ProcessID)
		}
		ctr.client.q.append(e.Id, func() {
			if err := ctr.client.backend.StateChanged(e.Id, st); err != nil {
				logrus.Errorf("libcontainerd: backend.StateChanged(): %v", err)
			}
			if e.Type == StatePause || e.Type == StateResume {
				ctr.pauseMonitor.handle(e.Type)
			}
			if e.Type == StateExit {
				if en := ctr.client.getExitNotifier(e.Id); en != nil {
					en.close()
				}
			}
		})

	default:
		logrus.Debugf("libcontainerd: event unhandled: %+v", e)
	}
	return nil
}

// discardFifos attempts to fully read the container fifos to unblock processes
// that may be blocked on the writer side.
func (ctr *container) discardFifos() {
	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	for _, i := range []int{syscall.Stdout, syscall.Stderr} {
		f, err := fifo.OpenFifo(ctx, ctr.fifo(i), syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			logrus.Warnf("error opening fifo %v for discarding: %+v", f, err)
			continue
		}
		go func() {
			io.Copy(ioutil.Discard, f)
		}()
	}
}
