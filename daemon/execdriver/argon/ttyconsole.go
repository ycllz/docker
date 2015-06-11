// +build windows

package argon

import (
	"github.com/docker/docker/pkg/hcsshim"
)

type TtyConsole struct {
	ID        string
	processId uint32
}

func NewTtyConsole(ID string, processId uint32) (*TtyConsole, error) {
	tty := &TtyConsole{ID: ID, processId: processId}
	return tty, nil
}

func (t *TtyConsole) Resize(ID string, h, w int) error {
	// We need to tell the virtual TTY via HCS that the client has resized.
	return hcsshim.ResizeTTY(t.ID, t.processId, h, w)
}

func (t *TtyConsole) Close() error {
	return nil
}
