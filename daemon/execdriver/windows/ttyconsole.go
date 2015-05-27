// +build windows

package windows

import (
	"github.com/docker/docker/pkg/hcsshim"
)

// TtyConsole is for when using a container interactively
type TtyConsole struct {
}

func NewTtyConsole() *TtyConsole {
	tty := &TtyConsole{}
	return tty
}

func (t *TtyConsole) Resize(h, w int) error {
	// TODO Windows: This is not implemented in HCS. Needs plumbing through
	// along with mechanism for buffering
	return hcsshim.ResizeTTY(h, w)
}

func (t *TtyConsole) Close() error {
	return nil
}
