// +build windows

package utils

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/natefinch/npipe"
)

// TODO: use the timeout? natefinch/npipe currently waits even if pipe doesn't exist, which is not what we want
func configureOSTransport(tr *http.Transport, proto, addr string, _ time.Duration) bool {
	if proto == "npipe" {
		win32Path := fmt.Sprintf(`\\%s`, filepath.FromSlash(addr))
		tr.Dial = func(_, _ string) (net.Conn, error) {
			return npipe.DialTimeout(win32Path, 50)
		}
		return true
	}
	return false
}
