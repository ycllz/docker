package utils

import (
	"net"
	"net/http"
	"time"
)

func ConfigureTCPTransport(tr *http.Transport, proto, addr string) {
	// Why 32? See https://github.com/docker/docker/pull/8035.
	timeout := 32 * time.Second
	if !configureOSTransport(tr, proto, addr, timeout) {
		tr.Proxy = http.ProxyFromEnvironment
		tr.Dial = (&net.Dialer{Timeout: timeout}).Dial
	}
}
