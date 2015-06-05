//+build windows

package windows

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/graphdriver"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/chrootarchive"
	"github.com/docker/docker/pkg/hcsshim"
	"github.com/docker/docker/pkg/ioutils"
)

func init() {
	graphdriver.Register("windows", InitDiff)
	graphdriver.Register("windowsfilter", InitFilter)
}

const (
	diffDriver = iota
	filterDriver
)

type WindowsGraphDriver struct {
	info       hcsshim.DriverInfo
	sync.Mutex // Protects concurrent modification to active
	active     map[string]int
}

// New returns a new WINDOWS Diff Disk driver.
func InitDiff(home string, options []string) (graphdriver.Driver, error) {
	log.Debugln("WindowsGraphDriver InitDiff() home", home)

	d := &WindowsGraphDriver{
		info: hcsshim.DriverInfo{
			HomeDir: home,
			Flavor:  diffDriver,
		},
		active: make(map[string]int),
	}

	return d, nil
}

// New returns a new WINDOWS Storage Filter driver.
func InitFilter(home string, options []string) (graphdriver.Driver, error) {
	log.Debugln("WindowsGraphDriver InitFilter() home", home)

	d := &WindowsGraphDriver{
		info: hcsshim.DriverInfo{
			HomeDir: home,
			Flavor:  filterDriver,
		},
		active: make(map[string]int),
	}

	return d, nil
}

func (d *WindowsGraphDriver) Info() hcsshim.DriverInfo {
	return d.info
}

func (d *WindowsGraphDriver) String() string {
	log.Debugln("WindowsGraphDriver String()")
	switch d.info.Flavor {
	case diffDriver:
		return "windows"
	case filterDriver:
		return "windowsfilter"
	default:
		panic("unsupported driver type")
	}
}

func (d *WindowsGraphDriver) Status() [][2]string {
	log.Debugln("WindowsGraphDriver Status()")
	return [][2]string{
		{"Windows", "To be confirmed what should be returned by Windows Storage Driver"},
	}
}

// Exists returns true if the given id is registered with
// this driver
func (d *WindowsGraphDriver) Exists(id string) bool {
	result, err := hcsshim.LayerExists(d.info, id)
	if err != nil {
		log.Errorf("LayerExists call failed: %s", err.Error())
		return false
	}

	log.Infoln("Exist result:", result)

	return result
}

func (d *WindowsGraphDriver) Create(id, parent string) error {
	log.Debugln("WindowsGraphDriver Create() id:", id, ", parent:", parent)

	return hcsshim.CreateLayer(d.info, id, parent)
}

func (d *WindowsGraphDriver) dir(id string) string {
	return filepath.Join(d.info.HomeDir, filepath.Base(id))
}

// Unmount and remove the dir information
func (d *WindowsGraphDriver) Remove(id string) error {
	log.Debugln("WindowsGraphDriver Remove()", id)

	return hcsshim.DestroyLayer(d.info, id)
}

// Return the rootfs path for the id
// This will mount the dir at it's given path
func (d *WindowsGraphDriver) Get(id, mountLabel string) (string, error) {
	log.Debugln("WindowsGraphDriver Get() id:", id, ", mountLabel:", mountLabel)

	var dir string

	d.Lock()
	defer d.Unlock()

	if d.active[id] == 0 {
		if err := hcsshim.ActivateLayer(d.info, id); err != nil {
			return "", err
		}
	}

	mountPath, err := hcsshim.GetLayerMountPath(d.info, id)
	if err != nil {
		return "", err
	}

	// If the layer has a mount path, use that. Otherwise, use the
	// folder path.
	if mountPath != "" {
		dir = mountPath
	} else {
		dir = d.dir(id)
	}

	d.active[id]++

	return dir, nil
}

func (d *WindowsGraphDriver) Put(id string) error {
	log.Debugln("WindowsGraphDriver Put() id:", id)

	d.Lock()
	defer d.Unlock()

	if d.active[id] > 1 {
		d.active[id]--
	} else if d.active[id] == 1 {
		if err := hcsshim.DeactivateLayer(d.info, id); err != nil {
			return err
		}
		delete(d.active, id)
	}

	return nil
}

func (d *WindowsGraphDriver) Cleanup() error {
	log.Debugln("WindowsGraphDriver Cleanup()")
	return nil
}

// Diff produces an archive of the changes between the specified
// layer and its parent layer which may be "".
func (d *WindowsGraphDriver) Diff(id, parent string) (arch archive.Archive, err error) {
	// Always include the diff disk for the given layer.
	if d.info.Flavor == diffDriver {
		diffFiles := []string{id + ".vhdx"}
		prevLayer := d.dir(parent)
		curParent, err := hcsshim.GetVhdParentPath(d.dir(id) + ".vhdx")
		if err != nil {
			return nil, err
		}
		for strings.EqualFold((prevLayer+".vhdx"), curParent) && curParent != "" {
			log.Debugf("Parent %s does not match parent %s.", (prevLayer + ".vhdx"), curParent)
			// Add that diff to the list of files.
			_, diffFile := filepath.Split(curParent)
			diffFiles = append(diffFiles, diffFile)
			curParent, err = hcsshim.GetVhdParentPath(curParent)
			if err != nil {
				return nil, err
			}
		}

		opts := &archive.TarOptions{
			IncludeFiles: diffFiles,
		}

		arch, err = archive.TarWithOptions(d.info.HomeDir, opts)
		if err != nil {
			return nil, err
		}
		return arch, nil
	} else if d.info.Flavor == filterDriver {
		// Perform a naive diff
		layerFs, err := d.Get(id, "")
		if err != nil {
			return nil, err
		}

		defer func() {
			if err != nil {
				d.Put(id)
			}
		}()

		if parent == "" {
			archive, err := archive.Tar(layerFs, archive.Uncompressed)
			if err != nil {
				return nil, err
			}
			return ioutils.NewReadCloserWrapper(archive, func() error {
				err := archive.Close()
				d.Put(id)
				return err
			}), nil
		}

		parentFs, err := d.Get(parent, "")
		if err != nil {
			return nil, err
		}
		defer d.Put(parent)

		changes, err := archive.ChangesDirs(layerFs, parentFs)
		if err != nil {
			return nil, err
		}

		archive, err := archive.ExportChanges(layerFs, changes)
		if err != nil {
			return nil, err
		}

		return ioutils.NewReadCloserWrapper(archive, func() error {
			err := archive.Close()
			d.Put(id)
			return err
		}), nil
	} else {
		return nil, fmt.Errorf("Unknown Windows driver flavor: %d", d.info.Flavor)
	}
}

// Changes produces a list of changes between the specified layer
// and its parent layer. If parent is "", then all changes will be ADD changes.
func (d *WindowsGraphDriver) Changes(id, parent string) ([]archive.Change, error) {
	layerFs, err := d.Get(id, "")
	if err != nil {
		return nil, err
	}
	defer d.Put(id)

	parentFs := ""

	if parent != "" {
		parentFs, err = d.Get(parent, "")
		if err != nil {
			return nil, err
		}
		defer d.Put(parent)
	}

	return archive.ChangesDirs(layerFs, parentFs)
}

// ApplyDiff extracts the changeset from the given diff into the
// layer with the specified id and parent, returning the size of the
// new layer in bytes.
func (d *WindowsGraphDriver) ApplyDiff(id, parent string, diff archive.ArchiveReader) (size int64, err error) {
	start := time.Now().UTC()
	log.Debugf("Start untar layer")

	destination := d.dir(id)
	if d.info.Flavor == diffDriver {
		destination = filepath.Dir(destination)
	}

	if size, err = chrootarchive.ApplyLayer(destination, diff); err != nil {
		return
	}
	log.Debugf("Untar time: %vs", time.Now().UTC().Sub(start).Seconds())

	return
}

// DiffSize calculates the changes between the specified layer
// and its parent and returns the size in bytes of the changes
// relative to its base filesystem directory.
func (d *WindowsGraphDriver) DiffSize(id, parent string) (size int64, err error) {
	changes, err := d.Changes(id, parent)
	if err != nil {
		return
	}

	layerFs, err := d.Get(id, "")
	if err != nil {
		return
	}
	defer d.Put(id)

	return archive.ChangesSize(layerFs, changes), nil
}

func (d *WindowsGraphDriver) CopyDiff(sourceId, id string, parentLayerPaths []string) error {
	d.Lock()
	defer d.Unlock()

	if d.info.Flavor == diffDriver && d.active[sourceId] != 0 {
		log.Warnf("Committing active id %s", sourceId)
		if err := hcsshim.DeactivateLayer(d.info, sourceId); err != nil {
			return err
		}
		defer func() {
			err := hcsshim.ActivateLayer(d.info, sourceId)
			if err != nil {
				log.Warnf("Failed to activate %s: %s", sourceId, err)
			}
		}()
	}

	return hcsshim.CopyLayer(d.info, sourceId, id, parentLayerPaths)
}

func (d *WindowsGraphDriver) LayerIdsToPaths(ids []string) []string {
	var paths []string
	for _, id := range ids {
		paths = append(paths, d.dir(id))
	}
	return paths
}
