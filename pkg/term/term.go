// Package term provides provides structures and helper functions to work with
// terminal (state, sizes).
package term

import (
	"io"
)

type Terminal interface {
	IsTerminal() bool
	GetWinsize() (*Winsize, error)
	SetWinsize(ws *Winsize) error
	SaveState() (*State, error)
	RestoreTerminal(state *State) error
	DisableEcho(state *State) error
	SetRawTerminal() (*State, error)
}

type TerminalReader interface {
	io.Reader
	Terminal
}

type TerminalReadCloser interface {
	io.ReadCloser
	Terminal
}

type TerminalWriter interface {
	io.Writer
	Terminal
}

// Winsize represents the size of the terminal window.
type Winsize struct {
	Height uint16
	Width  uint16
	x      uint16
	y      uint16
}
