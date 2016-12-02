package hcsshim

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	winio "github.com/Microsoft/go-winio"
	"github.com/docker/docker/pkg/archive"
)

type linuxLayerWriter struct {
	root         string
	parentRoots  []string
	destRoot     string
	currentFile  *os.File
	backupWriter *winio.BackupFileWriter
	tombstones   []string
	pathFixed    bool
	HasUtilityVM bool
	uvmDi        []dirInfo
	addedFiles   map[string]bool
	PendingLinks []pendingLink
}

// newlinuxLayerWriter returns a LayerWriter that can write the contaler layer
// transport format to disk.
func newLinuxLayerWriter(root string, parentRoots []string, destRoot string) *linuxLayerWriter {
	return &linuxLayerWriter{
		root:        root,
		parentRoots: parentRoots,
		destRoot:    destRoot,
		addedFiles:  make(map[string]bool),
	}
}

func (w *linuxLayerWriter) init() error {
	if !w.pathFixed {
		path, err := makeLongAbsPath(w.root)
		if err != nil {
			return err
		}
		for i, p := range w.parentRoots {
			w.parentRoots[i], err = makeLongAbsPath(p)
			if err != nil {
				return err
			}
		}
		destPath, err := makeLongAbsPath(w.destRoot)
		if err != nil {
			return err
		}
		w.root = path
		w.destRoot = destPath
		w.pathFixed = true
	}
	return nil
}

func (w *linuxLayerWriter) reset() {
	if w.backupWriter != nil {
		w.backupWriter.Close()
		w.backupWriter = nil
	}
	if w.currentFile != nil {
		w.currentFile.Close()
		w.currentFile = nil
	}
}

func (w *linuxLayerWriter) Add(name string, fileFullInfo *winio.FileFullInfo) error {
	fileInfo := &fileFullInfo.BasicInfo

	w.reset()
	err := w.init()
	if err != nil {
		return err
	}

	path := filepath.Join(w.root, name)
	if (fileInfo.FileAttributes & syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		err := os.Mkdir(path, 0)
		if err != nil {
			return err
		}
		path += ".$wcidirs$"
	}

	f, err := openFileOrDir(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.CREATE_NEW)
	if err != nil {
		return err
	}
	defer func() {
		if f != nil {
			f.Close()
			os.Remove(path)
		}
	}()

	strippedFi := *fileInfo
	strippedFi.FileAttributes = 0
	err = winio.SetFileBasicInfo(f, &strippedFi)
	if err != nil {
		return err
	}

	// The file attributes are written before the stream.
	err = binary.Write(f, binary.LittleEndian, uint32(fileInfo.FileAttributes))
	if err != nil {
		return err
	}

	w.currentFile = f
	w.addedFiles[name] = true
	f = nil
	return nil
}

func (w *linuxLayerWriter) AddLink(name string, target string) error {
	w.reset()
	err := w.init()
	if err != nil {
		return err
	}

	// Look for the this layer and all parent layers.
	roots := []string{w.destRoot}
	roots = append(roots, w.parentRoots...)

	// Find to try the target of the link in a previously added file. If that
	// fails, search in parent layers.
	var selectedRoot string
	if _, ok := w.addedFiles[target]; ok {
		selectedRoot = w.destRoot
	} else {
		for _, r := range roots {
			if _, err = os.Lstat(filepath.Join(r, target)); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
			} else {
				selectedRoot = r
				break
			}
		}
		if selectedRoot == "" {
			return fmt.Errorf("failed to find link target for '%s' -> '%s'", name, target)
		}
	}
	// The link can't be written until after the ImportLayer call.
	w.PendingLinks = append(w.PendingLinks, pendingLink{
		Path:   filepath.Join(w.destRoot, name),
		Target: filepath.Join(selectedRoot, target),
	})
	w.addedFiles[name] = true
	return nil
}

func (w *linuxLayerWriter) Remove(name string) error {
	// AKASH
	// Changed this around to support white out files on Linux
	// It's bascially the same thing as add.
	var i int
	for i = len(name) - 1; i >= 0; i-- {
		if name[i] == '\\' {
			break
		}
	}

	if i == len(name)-1 {
		return fmt.Errorf("Invalid path name: trailing slash")
	}

	wname := name[:i+1] + archive.WhiteoutPrefix + name[i+1:]

	_, err := os.Create(filepath.Join(w.root, wname))
	return err
}

func (w *linuxLayerWriter) Write(b []byte) (int, error) {
	if w.backupWriter == nil {
		if w.currentFile == nil {
			return 0, errors.New("closed")
		}
		return w.currentFile.Write(b)
	}
	return w.backupWriter.Write(b)
}

func (w *linuxLayerWriter) Close() error {
	w.reset()
	err := w.init()
	if err != nil {
		return err
	}
	tf, err := os.Create(filepath.Join(w.root, "tombstones.txt"))
	if err != nil {
		return err
	}
	defer tf.Close()
	_, err = tf.Write([]byte("\xef\xbb\xbfVersion 1.0\n"))
	if err != nil {
		return err
	}
	for _, t := range w.tombstones {
		_, err = tf.Write([]byte(filepath.Join(`\`, t) + "\n"))
		if err != nil {
			return err
		}
	}
	return nil
}
