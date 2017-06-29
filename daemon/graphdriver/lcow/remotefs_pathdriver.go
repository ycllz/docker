package lcow

import (
	"errors"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"path"

	"github.com/containerd/continuity/pathdriver"
)

var _ pathdriver.PathDriver = &lcowfs{}

// Continuity Path functions can be done locally
func (d *lcowfs) Join(path ...string) string {
	return pathpkg.Join(path...)
}

func (d *lcowfs) IsAbs(path string) bool {
	return pathpkg.IsAbs(path)
}

func (d *lcowfs) Rel(base, target string) (string, error) {
	// This is mostly copied from the Go filepath.Rel function since the
	// path package does not have Rel.
	baseClean, targetClean := d.Clean(base), d.Clean(target)

	// If one path is relative, but the other is absolute, we would need to
	// know the current directory figure out where the path actually is.
	if d.IsAbs(baseClean) != d.IsAbs(targetClean) {
		return "", errors.New("Rel: can't make " + target + " relative to " + base)
	}

	// Position base[b0:bi] and targ[t0:ti] at the first differing elements.
	bl := len(baseClean)
	tl := len(targetClean)
	var b0, bi, t0, ti int
	for {
		for bi < bl && baseClean[bi] != '/' {
			bi++
		}
		for ti < tl && targetClean[ti] != '/' {
			ti++
		}
		if targetClean[t0:ti] != baseClean[b0:bi] {
			break
		}
		if bi < bl {
			bi++
		}
		if ti < tl {
			ti++
		}
		b0 = bi
		t0 = ti
	}
	if baseClean[b0:bi] == ".." {
		return "", errors.New("Rel: can't make " + target + " relative to " + base)
	}

	if b0 != bl {
		// Base elements left. Must go up before going down.
		seps := strings.Count(baseClean[b0:bl], "/")
		size := 2 + seps*3
		if tl != t0 {
			size += 1 + tl - t0
		}
		buf := make([]byte, size)
		n := copy(buf, "..")
		for i := 0; i < seps; i++ {
			buf[n] = '/'
			copy(buf[n+1:], "..")
			n += 3
		}
		if t0 != tl {
			buf[n] = '/'
			copy(buf[n+1:], targetClean[t0:])
		}
		return string(buf), nil
	}
	return targetClean[t0:], nil
}

func (d *lcowfs) Base(path string) string {
	return pathpkg.Base(path)
}

func (d *lcowfs) Dir(path string) string {
	return pathpkg.Dir(path)
}

func (d *lcowfs) Clean(path string) string {
	return pathpkg.Clean(path)
}

func (d *lcowfs) Split(path string) (dir, file string) {
	return pathpkg.Split(path)
}

func (d *lcowfs) Separator() byte {
	return '/'
}

func (d *lcowfs) Abs(path string) (string, error) {
	// Abs is supposed to add the current working directory, which is meaningless in lcow.
	// So, return an error.
	return "", ErrNotSupported
}

func (d *lcowfs) Walk(root string, walkFn filepath.WalkFunc) error {
	// Implementation taken from the Go standard library
	info, err := d.Lstat(root)
	if err != nil {
		err = walkFn(root, nil, err)
	} else {
		err = d.walk(root, info, walkFn)
	}
	if err == filepath.SkipDir {
		return nil
	}
	return err
}

// walk recursively descends path, calling w.
func (d *lcowfs) walk(path string, info os.FileInfo, walkFn filepath.WalkFunc) error {
	err := walkFn(path, info, nil)
	if err != nil {
		if info.IsDir() && err == filepath.SkipDir {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	names, err := d.readDirNames(path)
	if err != nil {
		return walkFn(path, info, err)
	}

	for _, name := range names {
		filename := d.Join(path, name)
		fileInfo, err := d.Lstat(filename)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			err = d.walk(filename, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != filepath.SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

// readDirNames reads the directory named by dirname and returns
// a sorted list of directory entries.
func (d *lcowfs) readDirNames(dirname string) ([]string, error) {
	f, err := d.Open(dirname)
	if err != nil {
		return nil, err
	}
	files, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}

	names := make([]string, len(files), len(files))
	for i := range files {
		names[i] = files[i].Name()
	}

	sort.Strings(names)
	return names, nil
}

func (d *lcowfs) FromSlash(path string) string {
	return path
}

func (d *lcowfs) ToSlash(path string) string {
	return path
}

func (d *lcowfs) Match(pattern, name string) (matched bool, err error) {
	return path.Match(pattern, name)
}
