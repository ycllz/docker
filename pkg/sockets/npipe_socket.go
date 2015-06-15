// +build windows

package sockets

import (
	"fmt"
	"net"
	"path/filepath"

	"github.com/docker/docker/pkg/listenbuffer"
	"github.com/natefinch/npipe"
)

func NewWindowsNamedPipeSocket(path string, activate <-chan struct{}) (net.Listener, error) {
	l, err := npipe.Listen(fmt.Sprintf(`\\%s`, filepath.FromSlash(path)))
	if err != nil {
		return nil, err
	}
	return listenbuffer.NewListenBufferFromListener(l, activate), nil
}
