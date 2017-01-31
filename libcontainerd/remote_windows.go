package libcontainerd

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/locker"
	"github.com/docker/go-connections/sockets"

	"google.golang.org/grpc"
)

// Windows notes
//
// containerd functionality is enabled by setting DOCKER_WINDOWS_USE_CONTAINERD
// in the environment. THIS IS STILL UNDER DEVELOPMENT.  @jhowardmsft
//
// Windows operates differently to Linux in a number of key areas, hence the
// remote code is quite different.
//
// 1. Containerd is not started by docker. It is assumed it is running under
//    control of the Windows SCM (or manually running for development purposes).
//
// 2. To allow multiple daemons to run side by side (such as in the case of
//    CI, and a few niche scenarios), the connection is configurable. This is
//    because Windows cannot running DinD.
//
// 3. Windows has HCS to act as the "supervisor", hence containerd is really
//    just a thin wrapper into HCS. It has no need to make use of runc, shims,
//    reparenting (which doesn't make sense for Hyper-V containers anyway),
//    and/or other Unix-specific functionality.

// TODO - SPLIT THIS STRUCTURE INTO PLATFORM COMMON AND AGNOSTIC
type remote struct {
	useContainerD bool             // Are we using containerD ?
	rpcAddr       string           // Named pipe of containerd
	rpcConn       *grpc.ClientConn // Connection to containerd
	stateDir      string
}

func init() {
	if os.Getenv("DOCKER_WINDOWS_USE_CONTAINERD") != "" {
		logrus.Warnln("Using remote containerd on Windows - this is prototype and work in progress")
	}
}

func (r *remote) Client(b Backend) (Client, error) {
	c := &client{
		clientCommon: clientCommon{
			backend:    b,
			containers: make(map[string]*container),
			locker:     locker.New(),
		},
	}
	return c, nil
}

// Cleanup is a no-op on Windows unless using containerd.
func (r *remote) Cleanup() {
	if !r.useContainerD {
		return
	}
}

func (r *remote) UpdateOptions(opts ...RemoteOption) error {
	return nil
}

// New creates a fresh instance of libcontainerd remote.
func New(stateDir string, options ...RemoteOption) (_ Remote, err error) {
	r := &remote{useContainerD: false}
	if os.Getenv("DOCKER_WINDOWS_USE_CONTAINERD") == "" {
		return r, nil
	}

	logrus.Warnln("Using remote containerd on Windows - this is prototype and work in progress")
	r.useContainerD = true

	// Apply the options
	for _, option := range options {
		if err := option.Apply(r); err != nil {
			return nil, err
		}
	}

	if r.rpcAddr == "" {
		return nil, fmt.Errorf("containerd remote address is not configured")
	}

	dialOpts := append([]grpc.DialOption{grpc.WithInsecure()},
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return sockets.DialPipe(addr, 32*time.Second)
		}),
	)

	logrus.Debugln("dialing containerd at ", r.rpcAddr)
	conn, err := grpc.Dial(r.rpcAddr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("error connecting to containerd at %s: %v", r.rpcAddr, err)
	}
	r.rpcConn = conn
	logrus.Debugln("connected to containerd at", r.rpcAddr)

	return r, nil
}

// WithLiveRestore is a noop on windows.
func WithLiveRestore(v bool) RemoteOption {
	return nil
}

// WithContainerDPipe is the named pipe connection to the containerD service.
func WithContainerDPipe(p string) RemoteOption {
	return containerDPipe(p)
}

type containerDPipe string

func (p containerDPipe) Apply(r Remote) error {
	if remote, ok := r.(*remote); ok {
		remote.rpcAddr = string(p)
		return nil
	}
	return fmt.Errorf("containerDPipe option not supported for this remote")
}
