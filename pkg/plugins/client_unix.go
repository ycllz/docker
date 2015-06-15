// +build !windows

package plugins

import (
	"net"
	"net/http"
	"time"
)

func configureOSTransport(tr *http.Transport, proto, addr string, timeout time.Duration) bool {
	if proto == "unix" {
		// No need for compression in local communications.
		tr.DisableCompression = true
		tr.Dial = func(_, _ string) (net.Conn, error) {
			return net.DialTimeout(proto, addr, timeout)
		}	
		return true
	}
	return false
}

