// +build !windows

package opts

var DefaultLocalAddr  = "unix:///var/run/docker.sock" // Docker daemon by default always listens on the default unix socket
