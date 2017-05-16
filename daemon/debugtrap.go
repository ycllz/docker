package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/pkg/system"
	"github.com/pkg/errors"
)

const dataStructuresLogNameTemplate = "daemon-data-%s.log"

// dumpDaemon appends the daemon datastructures into file in dir and returns full path
// to that file.
func (d *Daemon) dumpDaemon(dir string) (string, error) {
	// Ensure we recover from a panic as we are doing this without any locking
	defer func() {
		recover()
	}()

	path := filepath.Join(dir, fmt.Sprintf(dataStructuresLogNameTemplate, strings.Replace(time.Now().Format(time.RFC3339), ":", "", -1)))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return "", errors.Wrap(err, "failed to open file to write the daemon datastructure dump")
	}
	defer f.Close()

	// TODO @jhowardmsft LCOW Support - this will require revisiting later, as well
	// as where it's used below to ensure all stores are dumped.
	platform := runtime.GOOS
	if platform == "windows" && system.LCOWSupported() {
		platform = "linux"
	}

	dump := struct {
		containers      interface{}
		names           interface{}
		links           interface{}
		execs           interface{}
		volumes         interface{}
		images          interface{}
		layers          interface{}
		imageReferences interface{}
		downloads       interface{}
		uploads         interface{}
		registry        interface{}
		plugins         interface{}
	}{
		containers:      d.containers,
		execs:           d.execCommands,
		volumes:         d.volumes,
		images:          d.stores[platform].imageStore,
		layers:          d.stores[platform].layerStore,
		imageReferences: d.stores[platform].referenceStore,
		downloads:       d.downloadManager,
		uploads:         d.uploadManager,
		registry:        d.RegistryService,
		plugins:         d.PluginStore,
		names:           d.nameIndex,
		links:           d.linkIndex,
	}

	spew.Fdump(f, dump) // Does not return an error
	f.Sync()
	return path, nil
}
