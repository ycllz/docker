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
	"github.com/docker/docker/image"
	"github.com/microsoft/hcsshim"
)

// SetupInitLayer populates a directory with mountpoints suitable
// for bind-mounting dockerinit into the container. T
func SetupInitLayer(initLayer string) error {
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

		if _, err := graph.Get(id); err != nil {
			// Add the image to the index.
			if err := graph.idIndex.Add(id); err != nil {
				return err
			}

			// If the image is not already created, create metadata for it.
			if _, err := os.Lstat(graph.imageRoot(id)); err != nil {
				img := &image.Image{
					ID:            id,
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

				// Create the placeholder entry in the graphdriver.
				if err := graph.driver.Create(img.ID, folderName); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
