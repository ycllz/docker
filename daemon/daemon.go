// Package daemon exposes the functions that occur on the host server
// that the Docker daemon is running.
//
// In implementing the various functions of the daemon, there is often
// a method-specific struct for configuring the runtime behavior.
package daemon

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/events"
	"github.com/docker/docker/daemon/execdriver"
	"github.com/docker/docker/daemon/graphdriver"
	"github.com/docker/docker/daemon/network"
	"github.com/docker/docker/image"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/broadcaster"
	"github.com/docker/docker/pkg/graphdb"
	"github.com/docker/docker/pkg/idtools"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/docker/docker/pkg/parsers/filters"
	"github.com/docker/docker/pkg/stringid"
	"github.com/docker/docker/pkg/stringutils"
	"github.com/docker/docker/pkg/truncindex"
	"github.com/docker/docker/runconfig"
	"github.com/docker/docker/utils"
	"github.com/docker/docker/volume/store"
)

var (
	validContainerNameChars   = utils.RestrictedNameChars
	validContainerNamePattern = utils.RestrictedNamePattern

	errSystemNotSupported = errors.New("The Docker daemon is not supported on this platform.")
)

type contStore struct {
	s map[string]*Container
	sync.Mutex
}

func (c *contStore) Add(id string, cont *Container) {
	c.Lock()
	c.s[id] = cont
	c.Unlock()
}

func (c *contStore) Get(id string) *Container {
	c.Lock()
	res := c.s[id]
	c.Unlock()
	return res
}

func (c *contStore) Delete(id string) {
	c.Lock()
	delete(c.s, id)
	c.Unlock()
}

func (c *contStore) List() []*Container {
	containers := new(History)
	c.Lock()
	for _, cont := range c.s {
		containers.Add(cont)
	}
	c.Unlock()
	containers.sort()
	return *containers
}

// Daemon holds information about the Docker daemon.
type Daemon struct {
	ID           string
	repository   string
	sysInitPath  string
	containers   *contStore
	execCommands *execStore

	idIndex          *truncindex.TruncIndex
	configStore      *Config
	containerGraphDB *graphdb.Database
	driver           graphdriver.Driver
	execDriver       execdriver.Driver
	statsCollector   *statsCollector
	defaultLogConfig runconfig.LogConfig
	EventsService    *events.Events
	volumes          *store.VolumeStore
	root             string
	shutdown         bool
	uidMaps          []idtools.IDMap
	gidMaps          []idtools.IDMap
}

// Get looks for a container using the provided information, which could be
// one of the following inputs from the caller:
//  - A full container ID, which will exact match a container in daemon's list
//  - A container name, which will only exact match via the GetByName() function
//  - A partial container ID prefix (e.g. short ID) of any length that is
//    unique enough to only return a single container object
//  If none of these searches succeed, an error is returned
func (daemon *Daemon) Get(prefixOrName string) (*Container, error) {
	if containerByID := daemon.containers.Get(prefixOrName); containerByID != nil {
		// prefix is an exact match to a full container ID
		return containerByID, nil
	}

	// GetByName will match only an exact name provided; we ignore errors
	if containerByName, _ := daemon.GetByName(prefixOrName); containerByName != nil {
		// prefix is an exact match to a full container Name
		return containerByName, nil
	}

	containerID, indexError := daemon.idIndex.Get(prefixOrName)
	if indexError != nil {
		// When truncindex defines an error type, use that instead
		if strings.Contains(indexError.Error(), "no such id") {
			return nil, nil
		}
		return nil, indexError
	}
	return daemon.containers.Get(containerID), nil
}

// Exists returns a true if a container of the specified ID or name exists,
// false otherwise.
func (daemon *Daemon) Exists(id string) bool {
	c, _ := daemon.Get(id)
	return c != nil
}

func (daemon *Daemon) containerRoot(id string) string {
	return filepath.Join(daemon.repository, id)
}

// Load reads the contents of a container from disk
// This is typically done at startup.
func (daemon *Daemon) load(id string) (*Container, error) {
	container := daemon.newBaseContainer(id)

	if err := container.fromDisk(); err != nil {
		return nil, err
	}

	if container.ID != id {
		return container, fmt.Errorf("Container %s is stored at %s", container.ID, id)
	}

	return container, nil
}

// Register makes a container object usable by the daemon as <container.ID>
func (daemon *Daemon) Register(container *Container) error {
	if container.daemon != nil || daemon.Exists(container.ID) {
		return fmt.Errorf("Container is already loaded")
	}
	if err := validateID(container.ID); err != nil {
		return err
	}
	if err := daemon.ensureName(container); err != nil {
		return err
	}

	container.daemon = daemon

	// Attach to stdout and stderr
	container.stderr = new(broadcaster.Unbuffered)
	container.stdout = new(broadcaster.Unbuffered)
	// Attach to stdin
	if container.Config.OpenStdin {
		container.stdin, container.stdinPipe = io.Pipe()
	} else {
		container.stdinPipe = ioutils.NopWriteCloser(ioutil.Discard) // Silently drop stdin
	}
	// done
	daemon.containers.Add(container.ID, container)

	// don't update the Suffixarray if we're starting up
	// we'll waste time if we update it for every container
	daemon.idIndex.Add(container.ID)

	if container.IsRunning() {
		logrus.Debugf("killing old running container %s", container.ID)
		// Set exit code to 128 + SIGKILL (9) to properly represent unsuccessful exit
		container.setStoppedLocking(&execdriver.ExitStatus{ExitCode: 137})
		// use the current driver and ensure that the container is dead x.x
		cmd := &execdriver.Command{
			ID: container.ID,
		}
		daemon.execDriver.Terminate(cmd)

		if err := container.unmountIpcMounts(); err != nil {
			logrus.Errorf("%s: Failed to umount ipc filesystems: %v", container.ID, err)
		}
		if err := container.Unmount(); err != nil {
			logrus.Debugf("unmount error %s", err)
		}
		if err := container.toDiskLocking(); err != nil {
			logrus.Errorf("Error saving stopped state to disk: %v", err)
		}
	}

	if err := daemon.verifyVolumesInfo(container); err != nil {
		return err
	}

	if err := container.prepareMountPoints(); err != nil {
		return err
	}

	return nil
}

func (daemon *Daemon) ensureName(container *Container) error {
	if container.Name == "" {
		name, err := daemon.generateNewName(container.ID)
		if err != nil {
			return err
		}
		container.Name = name

		if err := container.toDiskLocking(); err != nil {
			logrus.Errorf("Error saving container name to disk: %v", err)
		}
	}
	return nil
}

func (daemon *Daemon) restore() error {
	type cr struct {
		container  *Container
		registered bool
	}

	var (
		debug         = os.Getenv("DEBUG") != ""
		currentDriver = daemon.driver.String()
		containers    = make(map[string]*cr)
	)

	if !debug {
		logrus.Info("Loading containers: start.")
	}
	dir, err := ioutil.ReadDir(daemon.repository)
	if err != nil {
		return err
	}

	for _, v := range dir {
		id := v.Name()
		container, err := daemon.load(id)
		if !debug && logrus.GetLevel() == logrus.InfoLevel {
			fmt.Print(".")
		}
		if err != nil {
			logrus.Errorf("Failed to load container %v: %v", id, err)
			continue
		}

		// Ignore the container if it does not support the current driver being used by the graph
		if (container.Driver == "" && currentDriver == "aufs") || container.Driver == currentDriver {
			logrus.Debugf("Loaded container %v", container.ID)

			containers[container.ID] = &cr{container: container}
		} else {
			logrus.Debugf("Cannot load container %s because it was created with another graph driver.", container.ID)
		}
	}

	if entities := daemon.containerGraphDB.List("/", -1); entities != nil {
		for _, p := range entities.Paths() {
			if !debug && logrus.GetLevel() == logrus.InfoLevel {
				fmt.Print(".")
			}

			e := entities[p]

			if c, ok := containers[e.ID()]; ok {
				c.registered = true
			}
		}
	}

	group := sync.WaitGroup{}
	for _, c := range containers {
		group.Add(1)

		go func(container *Container, registered bool) {
			defer group.Done()

			if !registered {
				// Try to set the default name for a container if it exists prior to links
				container.Name, err = daemon.generateNewName(container.ID)
				if err != nil {
					logrus.Debugf("Setting default id - %s", err)
				}
			}

			if err := daemon.Register(container); err != nil {
				logrus.Errorf("Failed to register container %s: %s", container.ID, err)
				// The container register failed should not be started.
				return
			}

			// check the restart policy on the containers and restart any container with
			// the restart policy of "always"
			if daemon.configStore.AutoRestart && container.shouldRestart() {
				logrus.Debugf("Starting container %s", container.ID)

				if err := container.Start(); err != nil {
					logrus.Errorf("Failed to start container %s: %s", container.ID, err)
				}
			}
		}(c.container, c.registered)
	}
	group.Wait()

	if !debug {
		if logrus.GetLevel() == logrus.InfoLevel {
			fmt.Println()
		}
		logrus.Info("Loading containers: done.")
	}

	return nil
}

func (daemon *Daemon) mergeAndVerifyConfig(config *runconfig.Config, img *image.Image) error {
	if img != nil && img.Config != nil {
		if err := runconfig.Merge(config, img.Config); err != nil {
			return err
		}
	}
	if config.Entrypoint.Len() == 0 && config.Cmd.Len() == 0 {
		return fmt.Errorf("No command specified")
	}
	return nil
}

func (daemon *Daemon) generateIDAndName(name string) (string, string, error) {
	var (
		err error
		id  = stringid.GenerateNonCryptoID()
	)

	if name == "" {
		if name, err = daemon.generateNewName(id); err != nil {
			return "", "", err
		}
		return id, name, nil
	}

	if name, err = daemon.reserveName(id, name); err != nil {
		return "", "", err
	}

	return id, name, nil
}

func (daemon *Daemon) reserveName(id, name string) (string, error) {
	if !validContainerNamePattern.MatchString(name) {
		return "", fmt.Errorf("Invalid container name (%s), only %s are allowed", name, validContainerNameChars)
	}

	if name[0] != '/' {
		name = "/" + name
	}

	if _, err := daemon.containerGraphDB.Set(name, id); err != nil {
		if !graphdb.IsNonUniqueNameError(err) {
			return "", err
		}

		conflictingContainer, err := daemon.GetByName(name)
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf(
			"Conflict. The name %q is already in use by container %s. You have to remove (or rename) that container to be able to reuse that name.", strings.TrimPrefix(name, "/"),
			stringid.TruncateID(conflictingContainer.ID))

	}
	return name, nil
}

func (daemon *Daemon) generateNewName(id string) (string, error) {
	var name string
	for i := 0; i < 6; i++ {
		name = namesgenerator.GetRandomName(i)
		if name[0] != '/' {
			name = "/" + name
		}

		if _, err := daemon.containerGraphDB.Set(name, id); err != nil {
			if !graphdb.IsNonUniqueNameError(err) {
				return "", err
			}
			continue
		}
		return name, nil
	}

	name = "/" + stringid.TruncateID(id)
	if _, err := daemon.containerGraphDB.Set(name, id); err != nil {
		return "", err
	}
	return name, nil
}

func (daemon *Daemon) generateHostname(id string, config *runconfig.Config) {
	// Generate default hostname
	// FIXME: the lxc template no longer needs to set a default hostname
	if config.Hostname == "" {
		config.Hostname = id[:12]
	}
}

func (daemon *Daemon) getEntrypointAndArgs(configEntrypoint *stringutils.StrSlice, configCmd *stringutils.StrSlice) (string, []string) {
	cmdSlice := configCmd.Slice()
	if configEntrypoint.Len() != 0 {
		eSlice := configEntrypoint.Slice()
		return eSlice[0], append(eSlice[1:], cmdSlice...)
	}
	return cmdSlice[0], cmdSlice[1:]
}

func (daemon *Daemon) newContainer(name string, config *runconfig.Config, imgID string) (*Container, error) {
	var (
		id             string
		err            error
		noExplicitName = name == ""
	)
	id, name, err = daemon.generateIDAndName(name)
	if err != nil {
		return nil, err
	}

	daemon.generateHostname(id, config)
	entrypoint, args := daemon.getEntrypointAndArgs(config.Entrypoint, config.Cmd)

	base := daemon.newBaseContainer(id)
	base.Created = time.Now().UTC()
	base.Path = entrypoint
	base.Args = args //FIXME: de-duplicate from config
	base.Config = config
	base.hostConfig = &runconfig.HostConfig{}
	base.ImageID = imgID
	base.NetworkSettings = &network.Settings{IsAnonymousEndpoint: noExplicitName}
	base.Name = name
	base.Driver = daemon.driver.String()
	base.ExecDriver = daemon.execDriver.Name()

	return base, err
}

// GetFullContainerName returns a constructed container name. I think
// it has to do with the fact that a container is a file on disk and
// this is sort of just creating a file name.
func GetFullContainerName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("Container name cannot be empty")
	}
	if name[0] != '/' {
		name = "/" + name
	}
	return name, nil
}

// GetByName returns a container given a name.
func (daemon *Daemon) GetByName(name string) (*Container, error) {
	fullName, err := GetFullContainerName(name)
	if err != nil {
		return nil, err
	}
	entity := daemon.containerGraphDB.Get(fullName)
	if entity == nil {
		return nil, fmt.Errorf("Could not find entity for %s", name)
	}
	e := daemon.containers.Get(entity.ID())
	if e == nil {
		return nil, fmt.Errorf("Could not find container for entity id %s", entity.ID())
	}
	return e, nil
}

// GetEventFilter returns a filters.Filter for a set of filters
func (daemon *Daemon) GetEventFilter(filter filters.Args) *events.Filter {
	// incoming container filter can be name, id or partial id, convert to
	// a full container id
	for i, cn := range filter["container"] {
		c, err := daemon.Get(cn)
		if err != nil {
			filter["container"][i] = ""
		} else {
			filter["container"][i] = c.ID
		}
	}
	return events.NewFilter(filter, daemon.GetLabels)
}

// SubscribeToEvents returns the currently record of events, a channel to stream new events from, and a function to cancel the stream of events.
func (daemon *Daemon) SubscribeToEvents() ([]*jsonmessage.JSONMessage, chan interface{}, func()) {
	return daemon.EventsService.Subscribe()
}

// GetLabels for a container or image id
func (daemon *Daemon) GetLabels(id string) map[string]string {
	return nil
}

// children returns all child containers of the container with the
// given name. The containers are returned as a map from the container
// name to a pointer to Container.
func (daemon *Daemon) children(name string) (map[string]*Container, error) {
	name, err := GetFullContainerName(name)
	if err != nil {
		return nil, err
	}
	children := make(map[string]*Container)

	err = daemon.containerGraphDB.Walk(name, func(p string, e *graphdb.Entity) error {
		c, err := daemon.Get(e.ID())
		if err != nil {
			return err
		}
		children[p] = c
		return nil
	}, 0)

	if err != nil {
		return nil, err
	}
	return children, nil
}

// parents returns the names of the parent containers of the container
// with the given name.
func (daemon *Daemon) parents(name string) ([]string, error) {
	name, err := GetFullContainerName(name)
	if err != nil {
		return nil, err
	}

	return daemon.containerGraphDB.Parents(name)
}

func (daemon *Daemon) registerLink(parent, child *Container, alias string) error {
	fullName := filepath.Join(parent.Name, alias)
	if !daemon.containerGraphDB.Exists(fullName) {
		_, err := daemon.containerGraphDB.Set(fullName, child.ID)
		return err
	}
	return nil
}

// Shutdown stops the daemon.
func (daemon *Daemon) Shutdown() error {
	return nil
}

// Mount sets container.basefs
// (is it not set coming in? why is it unset?)
func (daemon *Daemon) Mount(container *Container) error {
	dir, err := daemon.driver.Get(container.ID, container.getMountLabel())
	if err != nil {
		return fmt.Errorf("Error getting container %s from driver %s: %s", container.ID, daemon.driver, err)
	}

	if container.basefs != dir {
		// The mount path reported by the graph driver should always be trusted on Windows, since the
		// volume path for a given mounted layer may change over time.  This should only be an error
		// on non-Windows operating systems.
		if container.basefs != "" && runtime.GOOS != "windows" {
			daemon.driver.Put(container.ID)
			return fmt.Errorf("Error: driver %s is returning inconsistent paths for container %s ('%s' then '%s')",
				daemon.driver, container.ID, container.basefs, dir)
		}
	}
	container.basefs = dir
	return nil
}

func (daemon *Daemon) unmount(container *Container) error {
	daemon.driver.Put(container.ID)
	return nil
}

func (daemon *Daemon) run(c *Container, pipes *execdriver.Pipes, startCallback execdriver.DriverCallback) (execdriver.ExitStatus, error) {
	hooks := execdriver.Hooks{
		Start: startCallback,
	}
	hooks.PreStart = append(hooks.PreStart, func(processConfig *execdriver.ProcessConfig, pid int, chOOM <-chan struct{}) error {
		return c.setNetworkNamespaceKey(pid)
	})
	return daemon.execDriver.Run(c.command, pipes, hooks)
}

func (daemon *Daemon) kill(c *Container, sig int) error {
	return daemon.execDriver.Kill(c.command, sig)
}

func (daemon *Daemon) stats(c *Container) (*execdriver.ResourceStats, error) {
	return daemon.execDriver.Stats(c.ID)
}

func (daemon *Daemon) subscribeToContainerStats(c *Container) (chan interface{}, error) {
	ch := daemon.statsCollector.collect(c)
	return ch, nil
}

func (daemon *Daemon) unsubscribeToContainerStats(c *Container, ch chan interface{}) error {
	daemon.statsCollector.unsubscribe(c, ch)
	return nil
}

func (daemon *Daemon) changes(container *Container) ([]archive.Change, error) {
	initID := fmt.Sprintf("%s-init", container.ID)
	return daemon.driver.Changes(container.ID, initID)
}

func (daemon *Daemon) diff(container *Container) (archive.Archive, error) {
	initID := fmt.Sprintf("%s-init", container.ID)
	return daemon.driver.Diff(container.ID, initID)
}

func (daemon *Daemon) createRootfs(container *Container) error {
	// Step 1: create the container directory.
	// This doubles as a barrier to avoid race conditions.
	rootUID, rootGID, err := idtools.GetRootUIDGID(daemon.uidMaps, daemon.gidMaps)
	if err != nil {
		return err
	}
	if err := idtools.MkdirAs(container.root, 0700, rootUID, rootGID); err != nil {
		return err
	}
	initID := fmt.Sprintf("%s-init", container.ID)
	if err := daemon.driver.Create(initID, container.ImageID); err != nil {
		return err
	}
	initPath, err := daemon.driver.Get(initID, "")
	if err != nil {
		return err
	}

	if err := setupInitLayer(initPath, rootUID, rootGID); err != nil {
		daemon.driver.Put(initID)
		return err
	}

	// We want to unmount init layer before we take snapshot of it
	// for the actual container.
	daemon.driver.Put(initID)

	if err := daemon.driver.Create(container.ID, initID); err != nil {
		return err
	}
	return nil
}

func (daemon *Daemon) config() *Config {
	return daemon.configStore
}

func (daemon *Daemon) systemInitPath() string {
	return daemon.sysInitPath
}

// GraphDriver returns the currently used driver for processing
// container layers.
func (daemon *Daemon) GraphDriver() graphdriver.Driver {
	return daemon.driver
}

// ExecutionDriver returns the currently used driver for creating and
// starting execs in a container.
func (daemon *Daemon) ExecutionDriver() execdriver.Driver {
	return daemon.execDriver
}

func (daemon *Daemon) containerGraph() *graphdb.Database {
	return daemon.containerGraphDB
}

// GetUIDGIDMaps returns the current daemon's user namespace settings
// for the full uid and gid maps which will be applied to containers
// started in this instance.
func (daemon *Daemon) GetUIDGIDMaps() ([]idtools.IDMap, []idtools.IDMap) {
	return daemon.uidMaps, daemon.gidMaps
}

// GetRemappedUIDGID returns the current daemon's uid and gid values
// if user namespaces are in use for this daemon instance.  If not
// this function will return "real" root values of 0, 0.
func (daemon *Daemon) GetRemappedUIDGID() (int, int) {
	uid, gid, _ := idtools.GetRootUIDGID(daemon.uidMaps, daemon.gidMaps)
	return uid, gid
}
