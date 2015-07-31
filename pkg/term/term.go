// Package term provides provides structures and helper functions to work with
// terminal (state, sizes).
package term

import (
	"io"
)

// Terminal provides the ability to set and query properties on a terminal.
type Terminal interface {
	IsTerminal() bool
	GetWinsize() (*Winsize, error)
	SetWinsize(ws *Winsize) error
	SaveState() (*State, error)
	RestoreTerminal(state *State) error
	DisableEcho(state *State) error
	SetRawTerminal() (*State, error)
}

// TerminalReader combines Terminal with io.Reader.
type TerminalReader interface {
	io.Reader
	Terminal
}

// TerminalReadCloser combines Terminal with io.ReadCloser.
type TerminalReadCloser interface {
	io.ReadCloser
	Terminal
}

// TerminalWriter combines Terimanl with io.Writer.
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
