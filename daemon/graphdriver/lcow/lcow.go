// +build windows

package lcow

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/graphdriver"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/idtools"
	"github.com/docker/docker/pkg/opengcs"
	"github.com/docker/docker/pkg/system"
)

// init registers the LCOW driver to the register.
func init() {
	graphdriver.Register("lcow", InitLCOW)
}

// Driver represents an LCOW graph driver.
type Driver struct {
	homeDir           string
	cachedSandboxFile string
	config            opengcs.Config
	uvm               hcsshim.Container
}

// InitLCOW returns a new LCOW storage filter driver.
func InitLCOW(home string, options []string, uidMaps, gidMaps []idtools.IDMap) (graphdriver.Driver, error) {
	logrus.Debugf("lcow driver init: %s", home)

	config, err := opengcs.DefaultConfig(filepath.Join(os.Getenv("ProgramFiles"), "lcow"), options)
	if err != nil {
		return nil, fmt.Errorf("failed to init LCOW driver - could not generate opengcs configuration: %s", err)
	}

	config.Name = "LinuxServiceVM"
	config.Svm = true

	d := &Driver{
		homeDir:           home,
		config:            config,
		cachedSandboxFile: filepath.Join(home, `cache\sandbox.vhdx`),
	}

	mode, warnings, err := config.Validate()
	if err != nil {
		// This is not fatal, as other drivers (eg WCOW) may still work.
		logrus.Warnf("LCOW driver does not have a valid configuration for communicating with the utility VM: %s.", err)
		return d, nil
	}
	if len(warnings) > 0 {
		for _, v := range warnings {
			// Again, these are not fatal as a) other drivers may still work, and
			// b) users can rectify the issue without the need to re-init the driver.
			logrus.Warnf("LCOW driver may not be fully operational: The following warning was generated during startup: %s", v)
		}
	}
	logrus.Infof("Default mode for LCOW driver: %s", mode)

	if err := idtools.MkdirAllAs(home, 0700, 0, 0); err != nil {
		return nil, fmt.Errorf("lcow failed to create '%s': %v", home, err)
	}

	// Cache for blank sandbox so don't have to pull it from the service VM
	if err := idtools.MkdirAllAs(filepath.Dir(d.cachedSandboxFile), 0700, 0, 0); err != nil {
		return nil, fmt.Errorf("lcow failed to create '%s': %v", home, err)
	}

	// Launch the service utility-VM
	// TODO @jhowardmsft. This will have to change in a future iteration.
	// a) We shouldn't be launching on daemon start. We should start on-demand
	// b) We will probably split to an SVM per container, not global, for RTM. That requires platform work though.
	if d.uvm, err = config.Create(); err != nil {
		return nil, fmt.Errorf("failed to init LCOW driver - could not create utility VM: %s", err)
	}

	return d, nil
}

// String returns the string representation of a driver. This should match
// the name the graph driver has been registered with.
func (d *Driver) String() string {
	return "lcow"
}

// Status returns the status of the driver.
func (d *Driver) Status() [][2]string {
	return [][2]string{
		{"LCOW", ""},
	}
}

// Exists returns true if the given id is registered with this driver.
func (d *Driver) Exists(id string) bool {
	logrus.Debugf("LCOWDriver Exists() id %s", id)
	_, err := os.Lstat(d.dir(id))
	return err == nil
}

// CreateReadWrite creates a layer that is writable for use as a container
// file system.
func (d *Driver) CreateReadWrite(id, parent string, opts *graphdriver.CreateOpts) error {
	logrus.Debugf("LCOWDriver CreateReadWrite() id %s", id)
	if err := d.Create(id, parent, opts); err != nil {
		return err
	}
	return opengcs.CreateSandbox(d.uvm, filepath.Join(d.dir(id), "sandbox.vhdx"), opengcs.DefaultSandboxSizeMB, d.cachedSandboxFile)
}

// Create creates a new read-only layer with the given id.
func (d *Driver) Create(id, parent string, opts *graphdriver.CreateOpts) error {
	logrus.Debugf("LCOWDriver Create() id %s", id)

	parentChain, err := d.getLayerChain(parent)
	if err != nil {
		return err
	}

	var layerChain []string
	if parent != "" {
		if !d.Exists(parent) {
			return fmt.Errorf("Cannot create layer with missing parent %s", parent)
		}
		layerChain = []string{d.dir(parent)}
	}
	layerChain = append(layerChain, parentChain...)

	layerPath := d.dir(id)
	logrus.Debugf("LCOWDriver Create id %s layerPath %s", id, layerPath)
	// Make sure the layers are created with the correct ACL so that VMs can access them.
	if err := system.MkdirAllWithACL(layerPath, 755, system.SddlNtvmAdministratorsLocalSystem); err != nil {
		return err
	}

	if err := d.setLayerChain(id, layerChain); err != nil {
		if err2 := os.RemoveAll(layerPath); err2 != nil {
			logrus.Warnf("Failed to remove layer %s: %s", layerPath, err2)
		}
		return err
	}
	return nil
}

// Remove unmounts and removes the dir information.
func (d *Driver) Remove(id string) error {
	logrus.Debugf("LCOWDriver Remove() id %s", id)

	tmpID := fmt.Sprintf("%s-removing", id)
	tmpLayerPath := d.dir(tmpID)
	layerPath := d.dir(id)

	logrus.Debugf("LCOWDriver Remove() id %s layerPath %s", id, layerPath)
	if err := os.Rename(layerPath, tmpLayerPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := os.RemoveAll(tmpLayerPath); err != nil {
		return err
	}

	return nil
}

// Get returns the rootfs path for the id.
func (d *Driver) Get(id, mountLabel string) (string, error) {
	logrus.Debugf("LCOWDriver Get() id %s mountLabel %s", id, mountLabel)
	return d.dir(id), nil
}

// Put adds a new layer to the driver.
func (d *Driver) Put(id string) error {
	logrus.Debugf("LCOWDriver Put() id %s", id)
	// TODO @gupta-ak. Service vm should unmount layer.
	return nil
}

// Cleanup ensures the information the driver stores is properly removed.
// We use this opportunity to cleanup any -removing folders which may be
// still left if the daemon was killed while it was removing a layer.
func (d *Driver) Cleanup() error {
	logrus.Debugf("LCOWDriver Cleanup()")

	items, err := ioutil.ReadDir(d.homeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Note we don't return an error below - it's possible the files
	// are locked. However, next time around after the daemon exits,
	// we likely will be able to to cleanup successfully. Instead we log
	// warnings if there are errors.
	for _, item := range items {
		if item.IsDir() && strings.HasSuffix(item.Name(), "-removing") {
			if err := os.RemoveAll(filepath.Join(d.homeDir, item.Name())); err != nil {
				logrus.Warnf("Failed to cleanup %s: %s", item.Name(), err)
			} else {
				logrus.Infof("Cleaned up %s", item.Name())
			}
		}
	}

	return nil
}

// Diff produces an archive of the changes between the specified
// layer and its parent layer which may be "".
// The layer should be mounted when calling this function
func (d *Driver) Diff(id, parent string) (_ io.ReadCloser, err error) {
	logrus.Debugf("LCOWDriver Diff() id %s parent %s", id, parent)
	// TODO @gupta-ak. graphdriver.Get() on the parent and then
	// Have the service vm take the difference between the two files.
	return nil, nil
}

// Changes produces a list of changes between the specified layer
// and its parent layer. If parent is "", then all changes will be ADD changes.
// The layer should not be mounted when calling this function.
func (d *Driver) Changes(id, parent string) ([]archive.Change, error) {
	logrus.Debugf("LCOWDriver Changes() id %s parent %s", id, parent)
	// TODO @gupta-ak. graphdriver.Get() on the parent and then
	// Have the service vm take the difference between the two files.
	return nil, nil
}

// ApplyDiff extracts the changeset from the given diff into the
// layer with the specified id and parent, returning the size of the
// new layer in bytes.
// The layer should not be mounted when calling this function
func (d *Driver) ApplyDiff(id, parent string, diff io.Reader) (int64, error) {
	logrus.Debugf("LCOWDriver ApplyDiff() id %s parent %s", id, parent)
	return opengcs.TarStreamToVHD(d.uvm, filepath.Join(d.homeDir, id, "layer.vhd"), diff)
}

// DiffSize calculates the changes between the specified layer
// and its parent and returns the size in bytes of the changes
// relative to its base filesystem directory.
func (d *Driver) DiffSize(id, parent string) (size int64, err error) {
	logrus.Debugf("LCOWDriver DiffSize() id %s parent %s", id, parent)
	// TODO @gupta-ak. graphdriver.Get() on the parent and then
	// Have the service vm take the difference between the two files.
	return 0, nil
}

// GetMetadata returns custom driver information.
func (d *Driver) GetMetadata(id string) (map[string]string, error) {
	logrus.Debugf("LCOWDriver GetMetadata() id %s", id)
	m := make(map[string]string)
	m["dir"] = d.dir(id)
	return m, nil
}

// dir returns the absolute path to the layer.
func (d *Driver) dir(id string) string {
	return filepath.Join(d.homeDir, filepath.Base(id))
}

// getLayerChain returns the layer chain information.
func (d *Driver) getLayerChain(id string) ([]string, error) {
	jPath := filepath.Join(d.dir(id), "layerchain.json")
	logrus.Debugf("LCOWDriver getLayerChain() id %s json %s", id, jPath)
	content, err := ioutil.ReadFile(jPath)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("Unable to read layerchain file %s: %s", jPath, err)
	}

	var layerChain []string
	err = json.Unmarshal(content, &layerChain)
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshall layerchain file %s: %s", jPath, err)
	}
	return layerChain, nil
}

// setLayerChain stores the layer chain information on disk.
func (d *Driver) setLayerChain(id string, chain []string) error {
	content, err := json.Marshal(&chain)
	if err != nil {
		return fmt.Errorf("Failed to marshall layerchain json - %s", err)
	}

	jPath := filepath.Join(d.dir(id), "layerchain.json")
	logrus.Debugf("LCOWDriver setLayerChain() id %s json %s", id, jPath)
	err = ioutil.WriteFile(jPath, content, 0600)
	if err != nil {
		return fmt.Errorf("Unable to write layerchain file - %s", err)
	}
	return nil
}
