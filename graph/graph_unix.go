// +build !windows

package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/docker/docker/image"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/system"
)

// setupInitLayer populates a directory with mountpoints suitable
// for bind-mounting dockerinit into the container. The mountpoint is simply an
// empty file at /.dockerinit
//
// This extra layer is used by all containers as the top-most ro layer. It protects
// the container from unwanted side-effects on the rw layer.
func SetupInitLayer(initLayer string) error {
	for pth, typ := range map[string]string{
		"/dev/pts":         "dir",
		"/dev/shm":         "dir",
		"/proc":            "dir",
		"/sys":             "dir",
		"/.dockerinit":     "file",
		"/.dockerenv":      "file",
		"/etc/resolv.conf": "file",
		"/etc/hosts":       "file",
		"/etc/hostname":    "file",
		"/dev/console":     "file",
		"/etc/mtab":        "/proc/mounts",
	} {
		parts := strings.Split(pth, "/")
		prev := "/"
		for _, p := range parts[1:] {
			prev = filepath.Join(prev, p)
			syscall.Unlink(filepath.Join(initLayer, prev))
		}

		if _, err := os.Stat(filepath.Join(initLayer, pth)); err != nil {
			if os.IsNotExist(err) {
				if err := system.MkdirAll(filepath.Join(initLayer, filepath.Dir(pth)), 0755); err != nil {
					return err
				}
				switch typ {
				case "dir":
					if err := system.MkdirAll(filepath.Join(initLayer, pth), 0755); err != nil {
						return err
					}
				case "file":
					f, err := os.OpenFile(filepath.Join(initLayer, pth), os.O_CREATE, 0755)
					if err != nil {
						return err
					}
					f.Close()
				default:
					if err := os.Symlink(typ, filepath.Join(initLayer, pth)); err != nil {
						return err
					}
				}
			} else {
				return err
			}
		}
	}

	// Layer is ready to use, if it wasn't before.
	return nil
}

func createRootFilesystemInDriver(graph *Graph, img *image.Image, layerData archive.ArchiveReader) error {
	if err := graph.driver.Create(img.ID, img.Parent); err != nil {
		return fmt.Errorf("Driver %s failed to create image rootfs %s: %s", graph.driver, img.ID, err)
	}
	return nil
}

func (graph *Graph) restoreBaseImages() ([]string, error) {
	return nil, nil
}
