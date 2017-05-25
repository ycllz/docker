package fs

import (
	"io"

	"github.com/containerd/continuity/fsdriver"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/chrootarchive"
	"github.com/docker/docker/pkg/symlink"
)

type localfs struct {
	root string
	fsdriver.Driver
}

func (lfs *localfs) Remote() bool {
	return false
}

func (lfs *localfs) HostPathName() string {
	return lfs.root
}

func (lfs *localfs) ExtractArchive(input io.Reader, path string, options *archive.TarOptions) error {
	return chrootarchive.Untar(input, path, options)
}

func (lfs *localfs) ArchivePath(path string, options *archive.TarOptions) (io.ReadCloser, error) {
	return archive.TarWithOptions(path, options)
}

func (lfs *localfs) ResolveFullPath(path string) (string, error) {
	cleanPath := cleanResourcePath(path)
	return symlink.FollowSymlinkInScope(lfs.Join(lfs.root, cleanPath), lfs.root)
}
