package daemon

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/chrootarchive"
	"github.com/opencontainers/runc/libcontainer/label"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/docker/runconfig"
	"github.com/docker/docker/volume"
	"github.com/docker/docker/volume/drivers"
)

var (
	// ErrVolumeReadonly is used to signal an error when trying to copy data into
	// a volume mount that is not writable.
	ErrVolumeReadonly = errors.New("mounted volume is marked read-only")
	// ErrVolumeInUse is a typed error returned when trying to remove a volume that is currently in use by a container
	ErrVolumeInUse = errors.New("volume is in use")
	// ErrNoSuchVolume is a typed error returned if the requested volume doesn't exist in the volume store
	ErrNoSuchVolume = errors.New("no such volume")
)

// mountPoint is the intersection point between a volume and a container. It
// specifies which volume is to be used and where inside a container it should
// be mounted.
type mountPoint struct {
	Name        string
	Destination string
	Driver      string
	RW          bool
	Volume      volume.Volume `json:"-"`
	Source      string
	Mode        string `json:"Relabel"` // Originally field was `Relabel`"
}

// BackwardsCompatible decides whether this mount point can be
// used in old versions of Docker or not.
// Only bind mounts and local volumes can be used in old versions of Docker.
func (m *mountPoint) BackwardsCompatible() bool {
	return len(m.Source) > 0 || m.Driver == volume.DefaultDriverName
}

// parseBindMount validates the configuration of mount information in runconfig is valid.
func parseBindMount(spec string, mountLabel string, config *runconfig.Config) (*mountPoint, error) {
	bind := &mountPoint{
		RW: true,
	}
	arr := strings.Split(spec, ":")

	switch len(arr) {
	case 2:
		bind.Destination = arr[1]
	case 3:
		bind.Destination = arr[1]
		mode := arr[2]
		if !volume.ValidMountMode(mode) {
			return nil, fmt.Errorf("invalid mode for volumes-from: %s", mode)
		}
		bind.RW = volume.ReadWrite(mode)
		// Mode field is used by SELinux to decide whether to apply label
		bind.Mode = mode
	default:
		return nil, fmt.Errorf("Invalid volume specification: %s", spec)
	}

	//validate the volumes destination path
	if !filepath.IsAbs(bind.Destination) {
		return nil, fmt.Errorf("Invalid volume destination path: %s mount path must be absolute.", bind.Destination)
	}

	name, source, err := parseVolumeSource(arr[0])
	if err != nil {
		return nil, err
	}

	if len(source) == 0 {
		bind.Driver = config.VolumeDriver
		if len(bind.Driver) == 0 {
			bind.Driver = volume.DefaultDriverName
		}
	} else {
		bind.Source = filepath.Clean(source)
	}

	bind.Name = name
	bind.Destination = filepath.Clean(bind.Destination)
	return bind, nil
}

// parseVolumesFrom ensure that the supplied volumes-from is valid.
func parseVolumesFrom(spec string) (string, string, error) {
	if len(spec) == 0 {
		return "", "", fmt.Errorf("malformed volumes-from specification: %s", spec)
	}

	specParts := strings.SplitN(spec, ":", 2)
	id := specParts[0]
	mode := "rw"

	if len(specParts) == 2 {
		mode = specParts[1]
		if !volume.ValidMountMode(mode) {
			return "", "", fmt.Errorf("invalid mode for volumes-from: %s", mode)
		}
	}
	return id, mode, nil
}

// Setup sets up a mount point by either mounting the volume if it is
// configured, or creating the source directory if supplied.
func (m *mountPoint) Setup() (string, error) {
	if m.Volume != nil {
		return m.Volume.Mount()
	}

	if len(m.Source) > 0 {
		if _, err := os.Stat(m.Source); err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			if err := system.MkdirAll(m.Source, 0755); err != nil {
				return "", err
			}
		}
		return m.Source, nil
	}

	return "", fmt.Errorf("Unable to setup mount point, neither source nor volume defined")
}

// hasResource checks whether the given absolute path for a container is in
// this mount point. If the relative path starts with `../` then the resource
// is outside of this mount point, but we can't simply check for this prefix
// because it misses `..` which is also outside of the mount, so check both.
func (m *mountPoint) hasResource(absolutePath string) bool {
	relPath, err := filepath.Rel(m.Destination, absolutePath)

	return err == nil && relPath != ".." && !strings.HasPrefix(relPath, fmt.Sprintf("..%c", filepath.Separator))
}

// Path returns the path of a volume in a mount point.
func (m *mountPoint) Path() string {
	if m.Volume != nil {
		return m.Volume.Path()
	}

	return m.Source
}

// copyExistingContents copies from the source to the destination and
// ensures the ownership is appropriately set.
func copyExistingContents(source, destination string) error {
	volList, err := ioutil.ReadDir(source)
	if err != nil {
		return err
	}
	if len(volList) > 0 {
		srcList, err := ioutil.ReadDir(destination)
		if err != nil {
			return err
		}
		if len(srcList) == 0 {
			// If the source volume is empty copy files from the root into the volume
			if err := chrootarchive.CopyWithTar(source, destination); err != nil {
				return err
			}
		}
	}
	return copyOwnership(source, destination)
}

// registerMountPoints initializes the container mount points with the configured volumes and bind mounts.
// It follows the next sequence to decide what to mount in each final destination:
//
// 1. Select the previously configured mount points for the containers, if any.
// 2. Select the volumes mounted from another containers. Overrides previously configured mount point destination.
// 3. Select the bind mounts set by the client. Overrides previously configured mount point destinations.
func (daemon *Daemon) registerMountPoints(container *Container, hostConfig *runconfig.HostConfig) error {
	binds := map[string]bool{}
	mountPoints := map[string]*mountPoint{}

	// 1. Read already configured mount points.
	for name, point := range container.MountPoints {
		mountPoints[name] = point
	}

	// 2. Read volumes from other containers.
	for _, v := range hostConfig.VolumesFrom {
		containerID, mode, err := parseVolumesFrom(v)
		if err != nil {
			return err
		}

		c, err := daemon.Get(containerID)
		if err != nil {
			return err
		}

		for _, m := range c.MountPoints {
			cp := &mountPoint{
				Name:        m.Name,
				Source:      m.Source,
				RW:          m.RW && volume.ReadWrite(mode),
				Driver:      m.Driver,
				Destination: m.Destination,
			}

			if len(cp.Source) == 0 {
				v, err := daemon.createVolume(cp.Name, cp.Driver, nil)
				if err != nil {
					return err
				}
				cp.Volume = v
			}

			mountPoints[cp.Destination] = cp
		}
	}

	// 3. Read bind mounts
	for _, b := range hostConfig.Binds {
		// #10618
		bind, err := parseBindMount(b, container.MountLabel, container.Config)
		if err != nil {
			return err
		}

		if binds[bind.Destination] {
			return fmt.Errorf("Duplicate bind mount %s", bind.Destination)
		}

		if len(bind.Name) > 0 && len(bind.Driver) > 0 {
			// create the volume
			v, err := daemon.createVolume(bind.Name, bind.Driver, nil)
			if err != nil {
				return err
			}
			bind.Volume = v
			bind.Source = v.Path()
			// bind.Name is an already existing volume, we need to use that here
			bind.Driver = v.DriverName()
			// Since this is just a named volume and not a typical bind, set to shared mode `z`
			if bind.Mode == "" {
				bind.Mode = "z"
			}
		}

		if err := label.Relabel(bind.Source, container.MountLabel, bind.Mode); err != nil {
			return err
		}
		binds[bind.Destination] = true
		mountPoints[bind.Destination] = bind
	}

	// Keep backwards compatible structures
	bcVolumes := map[string]string{}
	bcVolumesRW := map[string]bool{}
	for _, m := range mountPoints {
		if m.BackwardsCompatible() {
			bcVolumes[m.Destination] = m.Path()
			bcVolumesRW[m.Destination] = m.RW

			// This mountpoint is replacing an existing one, so the count needs to be decremented
			if mp, exists := container.MountPoints[m.Destination]; exists && mp.Volume != nil {
				daemon.volumes.Decrement(mp.Volume)
			}
		}
	}

	container.Lock()
	container.MountPoints = mountPoints
	container.Volumes = bcVolumes
	container.VolumesRW = bcVolumesRW
	container.Unlock()

	return nil
}

// createVolume creates a volume.
func (daemon *Daemon) createVolume(name, driverName string, opts map[string]string) (volume.Volume, error) {
	v, err := daemon.volumes.Create(name, driverName, opts)
	if err != nil {
		return nil, err
	}
	daemon.volumes.Increment(v)
	return v, nil
}

func newVolumeStore(vols []volume.Volume) *volumeStore {
	store := &volumeStore{
		vols: make(map[string]*volumeCounter),
	}
	for _, v := range vols {
		store.vols[v.Name()] = &volumeCounter{v, 0}
	}
	return store
}

// volumeStore is a struct that stores the list of volumes available and keeps track of their usage counts
type volumeStore struct {
	vols map[string]*volumeCounter
	mu   sync.Mutex
}

type volumeCounter struct {
	volume.Volume
	count int
}

func getVolumeDriver(name string) (volume.Driver, error) {
	if name == "" {
		name = volume.DefaultDriverName
	}
	return volumedrivers.Lookup(name)
}

// parseVolumeSource parses the origin sources that's mounted into the container.
func parseVolumeSource(spec string) (string, string, error) {
	if !filepath.IsAbs(spec) {
		return spec, "", nil
	}

	return "", spec, nil
}

// Create tries to find an existing volume with the given name or create a new one from the passed in driver
func (s *volumeStore) Create(name, driverName string, opts map[string]string) (volume.Volume, error) {
	s.mu.Lock()
	if vc, exists := s.vols[name]; exists {
		v := vc.Volume
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()

	vd, err := getVolumeDriver(driverName)
	if err != nil {
		return nil, err
	}

	v, err := vd.Create(name, opts)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.vols[v.Name()] = &volumeCounter{v, 0}
	s.mu.Unlock()

	return v, nil
}

// Get looks if a volume with the given name exists and returns it if so
func (s *volumeStore) Get(name string) (volume.Volume, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vc, exists := s.vols[name]
	if !exists {
		return nil, ErrNoSuchVolume
	}
	return vc.Volume, nil
}

// Remove removes the requested volume. A volume is not removed if the usage count is > 0
func (s *volumeStore) Remove(v volume.Volume) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := v.Name()
	vc, exists := s.vols[name]
	if !exists {
		return ErrNoSuchVolume
	}

	if vc.count != 0 {
		return ErrVolumeInUse
	}

	vd, err := getVolumeDriver(vc.DriverName())
	if err != nil {
		return err
	}
	if err := vd.Remove(vc.Volume); err != nil {
		return err
	}
	delete(s.vols, name)
	return nil
}

// Increment increments the usage count of the passed in volume by 1
func (s *volumeStore) Increment(v volume.Volume) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vc, exists := s.vols[v.Name()]
	if !exists {
		s.vols[v.Name()] = &volumeCounter{v, 1}
		return
	}
	vc.count++
	return
}

// Decrement decrements the usage count of the passed in volume by 1
func (s *volumeStore) Decrement(v volume.Volume) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vc, exists := s.vols[v.Name()]
	if !exists {
		return
	}
	vc.count--
	return
}

// Count returns the usage count of the passed in volume
func (s *volumeStore) Count(v volume.Volume) int {
	vc, exists := s.vols[v.Name()]
	if !exists {
		return 0
	}
	return vc.count
}

// List returns all the available volumes
func (s *volumeStore) List() []volume.Volume {
	var ls []volume.Volume
	for _, vc := range s.vols {
		ls = append(ls, vc.Volume)
	}
	return ls
}

// volumeToAPIType converts a volume.Volume to the type used by the remote API
func volumeToAPIType(v volume.Volume) *types.Volume {
	return &types.Volume{
		Name:       v.Name(),
		Driver:     v.DriverName(),
		Mountpoint: v.Path(),
	}
}
