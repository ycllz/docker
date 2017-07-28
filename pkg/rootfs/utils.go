package rootfs

import (
	"io"
	"io/ioutil"
	"os"
	"sort"
)

// Utility functions for rootfs operations that aren't provided by the
// continuity interface

// ReadFile works the same as ioutil.ReadFile with the rootFS abstraction
func ReadFile(r RootFS, filename string) ([]byte, error) {
	f, err := r.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// WriteFile works the same as ioutil.WriteFile with the rootFS abstraction
func WriteFile(r RootFS, filename string, data []byte, perm os.FileMode) error {
	f, err := r.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := f.Write(data)
	if err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}

	return nil
}

// ReadDir works the same as ioutil.ReadDir with the rootFS abstraction
func ReadDir(r RootFS, dirname string) ([]os.FileInfo, error) {
	f, err := r.Open(dirname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dirs, err := f.Readdir(-1)
	if err != nil {
		return nil, nil
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	return dirs, nil
}
