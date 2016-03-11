package libcontainerd

import (
	"github.com/docker/docker/libcontainerd/windowsoci"
	"io"
)

// process keeps the state for both main container process and exec process.
type process struct {
	client *client

	// id is the Container ID
	id string

	// This is a string identifier, not a PID (name is a little confusing IMO)
	processID string

	// On Windows, systemPid is the PID of the first process created in
	// a container, not the 'system' PID in the Linux context. In other words,
	// it's the PID returned by vmcompute.dll CreateProcessInComputeSystem()
	systemPid uint32

	// The following is stored as container.Start() in Windows
	// needs information that was originally passed in with the spec. This
	// avoids the start() function requiring a spec to be passed in
	// (and remembering the spec isn't available in the context of a restart
	// manager initiated start)

	ociProcess windowsoci.Process
}

func openReaderFromPipe(p io.ReadCloser) io.Reader {
	r, w := io.Pipe()
	go func() {
		if _, err := io.Copy(w, p); err != nil {
			r.CloseWithError(err)
		}
		w.Close()
		p.Close()
	}()
	return r
}
