package layer

import (
	"io"

	"github.com/docker/docker/pkg/archive"
)

func (rl *referencedRWLayer) TarStream() (io.ReadCloser, error) {
	rl.activityL.Lock()
	defer rl.activityL.Unlock()

	if rl.activityCount <= 0 {
		return nil, ErrNotSupported
	}

	return rl.mountedLayer.TarStream()
}

func (rl *referencedRWLayer) Changes() ([]archive.Change, error) {
	rl.activityL.Lock()
	defer rl.activityL.Unlock()

	if rl.activityCount <= 0 {
		return nil, ErrNotSupported
	}

	return rl.mountedLayer.Changes()
}
