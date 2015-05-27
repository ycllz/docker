// +build windows

package windows

// Note this is alpha code for the bring up of containers on Windows.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/pkg/parsers"
)

// This is a daemon development variable only and should not be
// used for running production containers on Windows.
var dummyMode bool

const (
	DriverName = "Windows 1854"
	Version    = "Alpha 10130"
)

type activeContainer struct {
	command *execdriver.Command
}

type driver struct {
	root             string
	initPath         string
	activeContainers map[string]*activeContainer
	sync.Mutex
}

func (d *driver) Name() string {
	return fmt.Sprintf("%s %s", DriverName, Version)
}

func NewDriver(root, initPath string, options []string) (*driver, error) {

	for _, option := range options {
		key, val, err := parsers.ParseKeyValueOpt(option)
		if err != nil {
			return nil, err
		}
		key = strings.ToLower(key)
		switch key {

		case "dummy":
			switch val {
			case "1":
				dummyMode = true
				logrus.Warn("Using dummy mode in Windows exec driver Exec(). This is for development use only!")
			}
		default:
			return nil, fmt.Errorf("Unknown exec driver option %s\n", key)
		}
	}

	return &driver{
		root:             root,
		initPath:         initPath,
		activeContainers: make(map[string]*activeContainer),
	}, nil
}
