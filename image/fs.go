package image

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/digest"
	"github.com/docker/docker/pkg/ioutils"
)

// IDWalkFunc is function called by StoreBackend.Walk
type IDWalkFunc func(id ID) error

// StoreBackend provides interface for image.Store persistence
type StoreBackend interface {
	Walk(f IDWalkFunc) error
	Get(id ID) ([]byte, error)
	Set(data []byte) (ID, error)
	Delete(id ID) error
	SetMetadata(id ID, key string, data []byte) error
	GetMetadata(id ID, key string) ([]byte, error)
	DeleteMetadata(id ID, key string) error
}

// fs implements StoreBackend using the filesystem.
type fs struct {
	sync.RWMutex
	root string
}

const (
	contentDirName  = "content"
	metadataDirName = "metadata"
)

// NewFSStoreBackend returns new filesystem based backend for image.Store
func NewFSStoreBackend(root string) (StoreBackend, error) {
	return newFSStore(root)
}

func newFSStore(root string) (*fs, error) {
	s := &fs{
		root: root,
	}
	if err := os.MkdirAll(filepath.Join(root, contentDirName, string(digest.Canonical)), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, metadataDirName, string(digest.Canonical)), 0700); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *fs) contentFile(id ID) string {
	dgst := digest.Digest(id)
	return filepath.Join(s.root, contentDirName, string(dgst.Algorithm()), dgst.Hex())
}

func (s *fs) metadataDir(id ID) string {
	dgst := digest.Digest(id)
	return filepath.Join(s.root, metadataDirName, string(dgst.Algorithm()), dgst.Hex())
}

// Walk calls the supplied callback for each image ID in the storage backend.
func (s *fs) Walk(f IDWalkFunc) error {
	// Only Canonical digest (sha256) is currently supported
	fmt.Println("Entered fs.go Walk()")
	fmt.Println(" - root=", s.root)
	fmt.Println(" - contentDirName", contentDirName)
	fmt.Println(" - digest.Canonical", string(digest.Canonical))
	s.RLock()
	dir, err := ioutil.ReadDir(filepath.Join(s.root, contentDirName, string(digest.Canonical)))
	s.RUnlock()
	if err != nil {
		return err
	}
	for _, v := range dir {
		dgst := digest.NewDigestFromHex(string(digest.Canonical), v.Name())
		fmt.Println("In loop: dgst", dgst.String())
		if err := dgst.Validate(); err != nil {
			logrus.Debugf("Skipping invalid digest %s: %s", dgst, err)
			continue
		}
		fmt.Println("Calling f(ID(dgst))...")
		if err := f(ID(dgst)); err != nil {
			debug.PrintStack()
			fmt.Println("Which failed")
			return err
		}
	}
	fmt.Println("Walk out of loop")
	return nil
}

// Get returns the content stored under a given ID.
func (s *fs) Get(id ID) ([]byte, error) {
	s.RLock()
	defer s.RUnlock()

	return s.get(id)
}

func (s *fs) get(id ID) ([]byte, error) {
	content, err := ioutil.ReadFile(s.contentFile(id))
	if err != nil {
		return nil, err
	}

	// todo: maybe optional
	if ID(digest.FromBytes(content)) != id {
		return nil, fmt.Errorf("failed to verify image: %v", id)
	}

	return content, nil
}

// Set stores content under a given ID.
func (s *fs) Set(data []byte) (ID, error) {
	s.Lock()
	defer s.Unlock()

	if len(data) == 0 {
		return "", fmt.Errorf("Invalid empty data")
	}

	id := ID(digest.FromBytes(data))
	if err := ioutils.AtomicWriteFile(s.contentFile(id), data, 0600); err != nil {
		return "", err
	}

	return id, nil
}

// Delete removes content and metadata files associated with the ID.
func (s *fs) Delete(id ID) error {
	s.Lock()
	defer s.Unlock()

	if err := os.RemoveAll(s.metadataDir(id)); err != nil {
		return err
	}
	if err := os.Remove(s.contentFile(id)); err != nil {
		return err
	}
	return nil
}

// SetMetadata sets metadata for a given ID. It fails if there's no base file.
func (s *fs) SetMetadata(id ID, key string, data []byte) error {
	s.Lock()
	defer s.Unlock()
	if _, err := s.get(id); err != nil {
		return err
	}

	baseDir := filepath.Join(s.metadataDir(id))
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(s.metadataDir(id), key), data, 0600)
}

// GetMetadata returns metadata for a given ID.
func (s *fs) GetMetadata(id ID, key string) ([]byte, error) {
	s.RLock()
	defer s.RUnlock()

	if _, err := s.get(id); err != nil {
		return nil, err
	}
	return ioutil.ReadFile(filepath.Join(s.metadataDir(id), key))
}

// DeleteMetadata removes the metadata associated with an ID.
func (s *fs) DeleteMetadata(id ID, key string) error {
	s.Lock()
	defer s.Unlock()

	return os.RemoveAll(filepath.Join(s.metadataDir(id), key))
}
