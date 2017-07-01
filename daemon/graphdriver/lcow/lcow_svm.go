package lcow

import (
	"errors"
	"sync"

	"github.com/Microsoft/hcsshim"
	"github.com/jhowardmsft/opengcs/gogcs/client"
)

// Code for all the service VM management for the LCOW graphdriver

var errVMisTerminating = errors.New("service VM is shutting down")
var errVMUnknown = errors.New("service vm id is unknown")
var errVMStillHasReference = errors.New("Attemping to delete a VM that is still being used")

// serviceVMMap is the struct representing the id -> service VM mapping
type serviceVMMap struct {
	sync.Mutex
	svms map[string]*serviceVM
}

// serviceVM is our internal structure representing an item in our
// map of service VMs we are maintaining.
type serviceVM struct {
	sync.Mutex                     // Serialises operations being performed in this service VM.
	scratchAttached bool           // Has a scratch been attached?
	config          *client.Config // Represents the service VM item.
	refCount        int            // refcount for VM

	// Indicate that the vm is started
	startStatus chan interface{}
	startError  error

	// Indicates that the vm is stopped
	stopStatus chan interface{}
	stopError  error

	// NOTE: It is OK to use a cache here because Windows does not support
	// restoring containers when the daemon dies.
	attachedVHDs        map[string]int // Map ref counting all the VHDS we've mounted/unmounted.
	attachedUnionMounts map[string]int // Map ref counting all the union mounts we've mounted
}

func newServiceVMMap() *serviceVMMap {
	return &serviceVMMap{
		svms: make(map[string]*serviceVM),
	}
}

// add will add an id to the service vm map. If the entry doesn't exist,
// then, then add will return an empty allocated service vm and set alreadyExists
// to false. If the id does exist, add will simply increment the reference count
func (svmMap *serviceVMMap) add(id string) (svm *serviceVM, alreadyExists bool, err error) {
	svmMap.Lock()
	defer svmMap.Unlock()
	if svm, ok := svmMap.svms[id]; ok {
		svm.Lock()
		defer svm.Unlock()
		if svm.refCount == 0 {
			return svm, true, errVMisTerminating
		}
		svm.refCount++
		return svm, true, nil
	}

	// Doesn't exist, so create an empty svm to put into map and return
	newSVM := newServiceVM()
	svmMap.svms[id] = newSVM
	svmMap.Unlock()
	return newSVM, false, nil
}

// get will simply return the svm given by the id. Works identical
// to the normal golang map.
func (svmMap *serviceVMMap) get(id string) (*serviceVM, bool) {
	svmMap.Lock()
	defer svmMap.Unlock()
	svm, ok := svmMap.svms[id]
	return svm, ok
}

// Reduces the ref count of the given ID from the map.
// When the refcount is 0, the function will return true. It will return false otherwise.
// If the id does not exist, the function will return an error.
func (svmMap *serviceVMMap) reduceRef(id string) (lastRef bool, _ error) {
	svmMap.Lock()
	defer svmMap.Unlock()

	svm, ok := svmMap.svms[id]
	svm.Lock()
	defer svm.Unlock()
	if !ok {
		return false, errVMUnknown
	}
	if svm.refCount == 0 {
		return false, errVMisTerminating
	}
	svm.refCount--
	return svm.refCount == 0, nil
}

// Deletes the given ID from the map. If the refcount is not 0 or the
// VM does not exist, then this function returns an error.
func (svmMap *serviceVMMap) deleteID(id string) error {
	svmMap.Lock()
	defer svmMap.Unlock()
	svm, ok := svmMap.svms[id]
	svm.Lock()
	defer svm.Unlock()
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
		startStatus:         make(chan interface{}),
		attachedVHDs:        make(map[string]int),
		attachedUnionMounts: make(map[string]int),
		refCount:            1,
		config:              &client.Config{},
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
	for _, mvd := range mvds {
		_, ok := svm.attachedVHDs[mvd.HostPath]
		if ok {
			svm.attachedVHDs[mvd.HostPath]++
			continue
		}

		svm.attachedVHDs[mvd.HostPath] = 1
		if err := svm.config.HotAddVhd(mvd.HostPath, mvd.ContainerPath, mvd.ReadOnly, !mvd.AttachOnly); err != nil {
			return err
		}
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
	for _, mvd := range mvds {
		hostPath := mvd.HostPath
		_, ok := svm.attachedVHDs[hostPath]
		if !ok {
			continue
		}

		svm.attachedVHDs[hostPath]--
		if svm.attachedVHDs[hostPath] != 0 {
			continue
		}

		// 0 ref count, so remove from VM and map
		if err := svm.config.HotRemoveVhd(hostPath); err != nil {
			return err
		}

		delete(svm.attachedVHDs, hostPath)
	}
	return nil
}

func (svm *serviceVM) createExt4VHDX(destFile string, sizeGB uint32, cacheFile string) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	err = svm.config.CreateExt4Vhdx(destFile, sizeGB, cacheFile)
	svm.Unlock()
	return nil
}

func (svm *serviceVM) createUnionMount(command string) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	svm.Unlock()
	return nil
}

func (svm *serviceVM) deleteUnionMount(mountname string) error {
	err := svm.getStartError()
	if err != nil {
		return err
	}

	svm.Lock()
	svm.Unlock()
	return nil
}
