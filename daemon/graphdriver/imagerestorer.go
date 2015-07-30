package graphdriver

import (
	"github.com/docker/docker/image"
	"github.com/docker/docker/pkg/archive"
)

// ImageRestorer interface allows the implementer to add a custom image to
// the graph and tagstore.
type ImageRestorer interface {
	RestoreCustomImages(tagger Tagger, recorder Recorder) ([]string, error)
}

// Tagger is an interface that exposes the TagStore.Tag function without needing
// to import graph.
type Tagger interface {
	Tag(repoName, tag, imageName string, force bool) error
}

// Recorder is an interface that exposes the Graph.Register and Graph.Exists
// functions without needing to import graph.
type Recorder interface {
	Exists(id string) bool
	Register(img *image.Image, layerData archive.Reader) error
}
