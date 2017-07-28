// +build windows

package lcow

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"io"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
	"github.com/jhowardmsft/opengcs/gogcs/client"
)

// Code for all the service VM management for the LCOW graphdriver

var errVMisTerminating = errors.New("service VM is shutting down")
var errVMUnknown = errors.New("service vm id is unknown")
var errVMStillHasReference = errors.New("Attemping to delete a VM that is still being used")

// serviceVMMap is the struct representing the id -> service VM mapping.
type serviceVMMap struct {
	sync.Mutex
	svms map[string]*serviceVMWithRef
}

// serviceVMWithRef is our internal structure representing an item in our
// map of service VMs we are maintaining.
type serviceVMWithRef struct {
	svm      *serviceVM // actual service vm object
	refCount int        // refcount for VM
}

type serviceVM struct {
	sync.Mutex                     // Serialises operations being performed in this service VM.
	scratchAttached bool           // Has a scratch been attached?
	config          *client.Config // Represents the service VM item.

	// Indicate that the vm is started
	startStatus chan interface{}
	startError  error

	// Indicates that the vm is stopped
	stopStatus chan interface{}
	stopError  error

	// NOTE: It is OK to use a cache here because Windows does not support
	// restoring containers when the daemon dies.
	attachedVHDs map[string]int // Map ref counting all the VHDS we've mounted/unmounted.
	unionMounts  map[string]int // Map ref counting all the union filesystems we mounted.
}

func newServiceVMMap() *serviceVMMap {
	return &serviceVMMap{
		svms: make(map[string]*serviceVMWithRef),
	}
}

// add will add an id to the service vm map. There are a couple of cases:
// 	- entry doesn't exist:
// 		- add id to map and return a new vm that the caller can manually configure+start
//	- entry does exist
//  	- return vm in map and add to ref count
//  - entry does exist but the ref count is 0
//		- return the svm and errVMisTerminating. Caller can call svm.getStopError() to wait for stop
func (svmMap *serviceVMMap) add(id string) (svm *serviceVM, alreadyExists bool, err error) {
	svmMap.Lock()
	defer svmMap.Unlock()
	if svm, ok := svmMap.svms[id]; ok {
		if svm.refCount == 0 {
			return svm.svm, true, errVMisTerminating
		}
		svm.refCount++
		return svm.svm, true, nil
	}

	// Doesn't exist, so create an empty svm to put into map and return
	newSVM := newServiceVM()
	svmMap.svms[id] = &serviceVMWithRef{
		svm:      newSVM,
		refCount: 1,
	}
	return newSVM, false, nil
}

// get will get the service vm from the map. There are a couple of cases:
// 	- entry doesn't exist:
// 		- return errVMUnknown
//	- entry does exist
//  	- return vm with no error
//  - entry does exist but the ref count is 0
//		- return the svm and errVMisTerminating. Caller can call svm.getStopError() to wait for stop
func (svmMap *serviceVMMap) get(id string) (*serviceVM, error) {
	svmMap.Lock()
	defer svmMap.Unlock()
	svm, ok := svmMap.svms[id]
	if !ok {
		return nil, errVMUnknown
	}
	if svm.refCount == 0 {
		return svm.svm, errVMisTerminating
	}
	return svm.svm, nil
}

// Reduces the ref count of the given ID from the map. There are a couple of cases:
// 	- entry doesn't exist:
// 		- return errVMUnknown
//  - entry does exist but the ref count is 0
//		- return the svm and errVMisTerminating. Caller can call svm.getStopError() to wait for stop
//	- entry does exist but ref count is 1
//  	- return vm and set lastRef to true. The caller can then stop the vm, delete the id from this map
//      - and execute svm.signalStopFinished to signal the threads that the svm has been terminated.
//	- entry does exist
//		- just reduce ref count and return svm
func (svmMap *serviceVMMap) reduceRef(id string) (_ *serviceVM, lastRef bool, _ error) {
	svmMap.Lock()
	defer svmMap.Unlock()

	svm, ok := svmMap.svms[id]
	if !ok {
		return nil, false, errVMUnknown
	}
	if svm.refCount == 0 {
		return svm.svm, false, errVMisTerminating
	}
	svm.refCount--
	return svm.svm, svm.refCount == 0, nil
}

// Works the same way as reduceRef, but sets ref count to 0 instead of decrementing it.
func (svmMap *serviceVMMap) reduceRefZero(id string) (*serviceVM, error) {
	svmMap.Lock()
	defer svmMap.Unlock()

	svm, ok := svmMap.svms[id]
	if !ok {
		return nil, errVMUnknown
	}
	if svm.refCount == 0 {
		return svm.svm, errVMisTerminating
	}
	svm.refCount = 0
	return svm.svm, nil
}

// Deletes the given ID from the map. If the refcount is not 0 or the
// VM does not exist, then this function returns an error.
func (svmMap *serviceVMMap) deleteID(id string) error {
	svmMap.Lock()
	defer svmMap.Unlock()
	svm, ok := svmMap.svms[id]
	if !ok {
		return errVMUnknown
	}
	if svm.refCount != 0 {
		return errVMStillHasReference
	}
	delete(svmMap.svms, id)
	return nil
}

func newServiceVM() *serviceVM {
	return &serviceVM{
		startStatus:  make(chan interface{}),
		stopStatus:   make(chan interface{}),
		attachedVHDs: make(map[string]int),
		unionMounts:  make(map[string]int),
		config:       &client.Config{},
	}
}

func (svm *serviceVM) signalStartFinished(err error) {
	svm.Lock()
	svm.startError = err
	svm.Unlock()
	close(svm.startStatus)
}

func (svm *serviceVM) getStartError() error {
	<-svm.startStatus
	svm.Lock()
	defer svm.Unlock()
	return svm.startError
}

func (svm *serviceVM) signalStopFinished(err error) {
	svm.Lock()
	svm.stopError = err
	svm.Unlock()
	close(svm.stopStatus)
}

func (svm *serviceVM) getStopError() error {
	<-svm.stopStatus
	svm.Lock()
	defer svm.Unlock()
	return svm.stopError
}

func (svm *serviceVM) hotAddVHDs(mvds ...hcsshim.MappedVirtualDisk) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	defer svm.Unlock()
	return svm.hotAddVHDsNoLock(mvds...)
}

func (svm *serviceVM) hotAddVHDsNoLock(mvds ...hcsshim.MappedVirtualDisk) error {
	for i, mvd := range mvds {
		_, ok := svm.attachedVHDs[mvd.HostPath]
		if ok {
			svm.attachedVHDs[mvd.HostPath]++
			continue
		}

		if err := svm.config.HotAddVhd(mvd.HostPath, mvd.ContainerPath, mvd.ReadOnly, !mvd.AttachOnly); err != nil {
			svm.hotRemoveVHDsNoLock(mvds[:i]...)
			return err
		}
		svm.attachedVHDs[mvd.HostPath] = 1
	}
	return nil
}

func (svm *serviceVM) hotRemoveVHDs(mvds ...hcsshim.MappedVirtualDisk) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	defer svm.Unlock()
	return svm.hotRemoveVHDsNoLock(mvds...)
}

func (svm *serviceVM) hotRemoveVHDsNoLock(mvds ...hcsshim.MappedVirtualDisk) error {
	var retErr error
	for _, mvd := range mvds {
		hostPath := mvd.HostPath
		_, ok := svm.attachedVHDs[hostPath]
		if !ok {
			continue
		}

		if svm.attachedVHDs[hostPath] != 1 {
			svm.attachedVHDs[hostPath]--
			continue
		}

		// last VHD, so remove from VM and map
		err := svm.config.HotRemoveVhd(hostPath)
		if err == nil {
			delete(svm.attachedVHDs, hostPath)
		} else {
			// Take note of the error, but still continue to remove the other VHDs
			logrus.Warnf("Failed to hot remove %s: %s", hostPath, err)
			if retErr == nil {
				retErr = err
			}
		}
	}
	return retErr
}

func (svm *serviceVM) createExt4VHDX(destFile string, sizeGB uint32, cacheFile string) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	defer svm.Unlock()
	return svm.config.CreateExt4Vhdx(destFile, sizeGB, cacheFile)
}

func (svm *serviceVM) createUnionMount(mountName string, mvds ...hcsshim.MappedVirtualDisk) (err error) {
	if len(mvds) == 0 {
		return fmt.Errorf("createUnionMount: error must have atleast 1 layer")
	}

	err = svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	defer svm.Unlock()
	_, ok := svm.unionMounts[mountName]
	if ok {
		svm.unionMounts[mountName]++
		return nil
	}

	var lowerLayers []string
	if mvds[0].ReadOnly {
		lowerLayers = append(lowerLayers, mvds[0].ContainerPath)
	}

	for i := 1; i < len(mvds); i++ {
		lowerLayers = append(lowerLayers, mvds[i].ContainerPath)
	}

	logrus.Debugf("Doing the overlay mount with union directory=%s", mountName)
	err = svm.runProcess(fmt.Sprintf("mkdir -p %s", mountName), nil, nil, nil)
	if err != nil {
		return err
	}

	var cmd string
	if mvds[0].ReadOnly {
		// Readonly overlay
		cmd = fmt.Sprintf("mount -t overlay overlay -olowerdir=%s %s",
			strings.Join(lowerLayers, ","),
			mountName)
	} else {
		upper := fmt.Sprintf("%s/upper", mvds[0].ContainerPath)
		work := fmt.Sprintf("%s/work", mvds[0].ContainerPath)

		err = svm.runProcess(fmt.Sprintf("mkdir -p %s %s", upper, work), nil, nil, nil)
		if err != nil {
			return err
		}

		cmd = fmt.Sprintf("mount -t overlay overlay -olowerdir=%s,upperdir=%s,workdir=%s %s",
			strings.Join(lowerLayers, ","),
			upper,
			work,
			mountName)
	}

	logrus.Debugf("createUnionMount: Executing mount=%s", cmd)
	err = svm.runProcess(cmd, nil, nil, nil)
	if err != nil {
		return err
	}

	svm.unionMounts[mountName] = 1
	return nil
}

func (svm *serviceVM) deleteUnionMount(mountName string, disks ...hcsshim.MappedVirtualDisk) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	defer svm.Unlock()
	_, ok := svm.unionMounts[mountName]
	if !ok {
		return nil
	}

	if svm.unionMounts[mountName] != 1 {
		svm.unionMounts[mountName]--
		return nil
	}

	logrus.Debugf("Removing union mount %s", mountName)
	err = svm.runProcess(fmt.Sprintf("umount %s", mountName), nil, nil, nil)
	if err != nil {
		return err
	}

	delete(svm.unionMounts, mountName)
	return nil
}

func (svm *serviceVM) runProcess(command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	process, err := svm.config.RunProcess(command, stdin, stdout, stderr)
	if err != nil {
		return err
	}
	defer process.Close()

	process.WaitTimeout(time.Duration(int(time.Second) * svm.config.UvmTimeoutSeconds))
	exitCode, err := process.ExitCode()
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return fmt.Errorf("svm.runProcess: command %s failed with exit code %d", command, exitCode)
	}
	return nil
}
