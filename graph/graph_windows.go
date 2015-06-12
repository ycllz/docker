// +build windows

package graph

import (
	"fmt"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/graphdriver/windows"
	"github.com/docker/docker/image"
	"github.com/docker/docker/pkg/archive"
)

// setupInitLayer populates a directory with mountpoints suitable
// for bind-mounting dockerinit into the container. T
func SetupInitLayer(initLayer string) error {
	return nil
}

func createRootFilesystemInDriver(graph *Graph, img *image.Image, layerData archive.ArchiveReader) error {

	if wd, ok := graph.driver.(*windows.WindowsGraphDriver); ok && img.Container != "" && layerData == nil {
		logrus.Debugf("Copying from container %s.", img.Container)

		var ids []string
		if img.Parent != "" {
			parentImg, err := graph.Get(img.Parent)
			if err != nil {
				return err
			}

			ids, err = graph.ParentLayerIds(parentImg)
			if err != nil {
				return err
			}
		}

		if err := wd.CopyDiff(img.Container, img.ID, wd.LayerIdsToPaths(ids)); err != nil {
			return fmt.Errorf("Driver %s failed to copy image rootfs %s: %s", graph.driver, img.Container, err)
		}
	} else {
		// This fallback allows the use of VFS during daemon development.
		if err := graph.driver.Create(img.ID, img.Parent); err != nil {
			return fmt.Errorf("Driver %s failed to create image rootfs %s: %s", graph.driver, img.ID, err)
		}
	}
	return nil
}

func (graph *Graph) restoreBaseImages() ([]string, error) {
	// TODO Windows. This needs implementing (@swernli)
	return nil, nil
}

// ParentLayerIds returns a list of all parent image IDs for the given image.
func (graph *Graph) ParentLayerIds(img *image.Image) (ids []string, err error) {
	for i := img; i != nil && err == nil; i, err = graph.GetParent(i) {
		ids = append(ids, i.ID)
	}

	return
}
