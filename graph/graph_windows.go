// +build windows

package graph

import (
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/autogen/dockerversion"
	"github.com/docker/docker/daemon/graphdriver/windows"
	"github.com/docker/docker/pkg/archive"
	"github.com/microsoft/hcsshim"
)

// setupInitLayer populates a directory with mountpoints suitable
// for bind-mounting dockerinit into the container. T
func SetupInitLayer(initLayer string) error {
	return nil
}

func createRootFilesystemInDriver(graph *Graph, img *Image, layerData archive.ArchiveReader) error {
	if wd, ok := graph.driver.(*windows.WindowsGraphDriver); ok {
		if img.Container != "" && layerData == nil {
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
		} else if img.Parent == "" {
			if err := graph.driver.Create(img.ID, img.Parent); err != nil {
				return fmt.Errorf("Driver %s failed to create image rootfs %s: %s", graph.driver, img.ID, err)
			}
		}
	} else {
		// This fallback allows the use of VFS during daemon development.
		if err := graph.driver.Create(img.ID, img.Parent); err != nil {
			return fmt.Errorf("Driver %s failed to create image rootfs %s: %s", graph.driver, img.ID, err)
		}
	}
	return nil
}

func (graph *Graph) RestoreBaseImages(ts *TagStore) error {
	strData, err := hcsshim.GetSharedBaseImages()
	if err != nil {
		return fmt.Errorf("Failed to restore base images: %s", err)
	}

	rawData := []byte(strData)

	type imageInfo struct {
		Name        string
		Version     string
		Path        string
		Size        int64
		CreatedTime time.Time
	}
	type imageInfoList struct {
		Images []imageInfo
	}
	var infoData imageInfoList

	if err = json.Unmarshal(rawData, &infoData); err != nil {
		err = fmt.Errorf("JSON unmarshal returned error=%s", err)
		logrus.Error(err)
		return err
	}

	for _, imageData := range infoData.Images {
		_, folderName := filepath.Split(imageData.Path)

		// Use crypto hash of the foldername to generate a docker style id.
		h := sha512.Sum384([]byte(folderName))
		id := fmt.Sprintf("%x", h[:32])

		// Add the image to the index.
		if err := graph.idIndex.Add(id); err != nil {
			return err
		}

		// If the image is not already created, create metadata for it.
		if _, err := os.Lstat(graph.imageRoot(id)); err != nil {
			img := &Image{
				ID:            id,
				LayerID:       folderName,
				Created:       imageData.CreatedTime,
				DockerVersion: dockerversion.VERSION,
				Author:        "Microsoft",
				Architecture:  runtime.GOARCH,
				OS:            runtime.GOOS,
			}

			tmp, err := graph.mktemp("")
			defer os.RemoveAll(tmp)
			if err != nil {
				return fmt.Errorf("mktemp failed: %s", err)
			}

			f, err := os.OpenFile(jsonPath(tmp), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0600))
			if err != nil {
				return err
			}

			if err := json.NewEncoder(f).Encode(img); err != nil {
				f.Close()
				return err
			}
			f.Close()

			graph.saveSize(tmp, int(imageData.Size))

			if err := os.Rename(tmp, graph.imageRoot(img.ID)); err != nil {
				return err
			}

			// Create tags for the new image.
			if err := ts.Tag(strings.ToLower(imageData.Name), imageData.Version, img.ID, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// ParentLayerIds returns a list of all parent image IDs for the given image.
func (graph *Graph) ParentLayerIds(img *Image) (ids []string, err error) {
	for i := img; i != nil && err == nil; i, err = graph.GetParent(i) {
		id := i.ID
		if i.LayerID != "" {
			id = i.LayerID
		}
		ids = append(ids, id)
	}

	return
}

// storeImage stores file system layer data for the given image to the
// graph's storage driver. Image metadata is stored in a file
// at the specified root directory.
func (graph *Graph) storeImage(img *Image, layerData archive.ArchiveReader, root string) (err error) {

	if wd, ok := graph.driver.(*windows.WindowsGraphDriver); ok {
		// Store the layer. If layerData is not nil and this isn't a base image,
		// unpack it into the new layer
		if layerData != nil && img.Parent != "" {
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

			if img.Size, err = wd.Import(img.ID, layerData, wd.LayerIdsToPaths(ids)); err != nil {
				return err
			}
		}

		if err := graph.saveSize(root, int(img.Size)); err != nil {
			return err
		}

		f, err := os.OpenFile(jsonPath(root), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0600))
		if err != nil {
			return err
		}

		defer f.Close()

		return json.NewEncoder(f).Encode(img)
	} else {
		// We keep this functionality here so that we can still work with the
		// VFS driver during development. This will not be used for actual running
		// of Windows containers. Without this code, it would not be possible to
		// docker pull using the VFS driver.

		// Store the layer. If layerData is not nil, unpack it into the new layer
		if layerData != nil {
			if img.Size, err = graph.driver.ApplyDiff(img.ID, img.Parent, layerData); err != nil {
				return err
			}
		}

		if err := graph.saveSize(root, int(img.Size)); err != nil {
			return err
		}

		f, err := os.OpenFile(jsonPath(root), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(0600))
		if err != nil {
			return err
		}

		defer f.Close()

		return json.NewEncoder(f).Encode(img)
	}
}

// TarLayer returns a tar archive of the image's filesystem layer.
func (graph *Graph) TarLayer(img *Image) (arch archive.Archive, err error) {
	if wd, ok := graph.driver.(*windows.WindowsGraphDriver); ok {
		var imgId string
		var ids []string
		if img.Parent != "" {
			imgId = img.ID
			parentImg, err := graph.Get(img.Parent)
			if err != nil {
				return nil, err
			}

			ids, err = graph.ParentLayerIds(parentImg)
			if err != nil {
				return nil, err
			}
		} else {
			imgId = img.LayerID
		}

		return wd.Export(imgId, wd.LayerIdsToPaths(ids))
	} else {
		// We keep this functionality here so that we can still work with the VFS
		// driver during development. VFS is not supported (and just will not work)
		// for Windows containers.
		return graph.driver.Diff(img.ID, img.Parent)
	}
}
