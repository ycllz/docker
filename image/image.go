package image

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/docker/docker/pkg/version"
	"github.com/docker/docker/runconfig"
)

var validHex = regexp.MustCompile(`^([a-f0-9]{64})$`)

// noFallbackMinVersion is the minimum version for which v1compatibility
// information will not be marshaled through the Image struct to remove
// blank fields.
var noFallbackMinVersion = version.Version("1.8.3")

// Descriptor provides the information necessary to register an image in
// the graph.
type Descriptor interface {
	ID() string
	Parent() string
	MarshalConfig() ([]byte, error)
}

// Image stores the image configuration.
// All fields in this struct must be marked `omitempty` to keep getting
// predictable hashes from the old `v1Compatibility` configuration.
type Image struct {
	// ID a unique 64 character identifier of the image
	ID string `json:"id,omitempty"`
	// Parent id of the image
	Parent string `json:"parent,omitempty"`
	// Comment user added comment
	Comment string `json:"comment,omitempty"`
	// Created timestamp when image was created
	Created time.Time `json:"created"`
	// Container is the id of the container used to commit
	Container string `json:"container,omitempty"`
	// ContainerConfig  is the configuration of the container that is committed into the image
	ContainerConfig runconfig.Config `json:"container_config,omitempty"`
	// DockerVersion specifies version on which image is built
	DockerVersion string `json:"docker_version,omitempty"`
	// Author of the image
	Author string `json:"author,omitempty"`
	// Config is the configuration of the container received from the client
	Config *runconfig.Config `json:"config,omitempty"`
	// Architecture is the hardware that the image is build and runs on
	Architecture string `json:"architecture,omitempty"`
	// OS is the operating system used to build and run the image
	OS string `json:"os,omitempty"`
	// Size is the total size of the image including all layers it is composed of
	Size int64 `json:",omitempty"` // capitalized for backwards compatibility
}

// NewImgJSON creates an Image configuration from json.
func NewImgJSON(src []byte) (*Image, error) {
	return nil, nil
}

// ValidateID checks whether an ID string is a valid image ID.
func ValidateID(id string) error {
	return nil
}

func rawJSON(value interface{}) *json.RawMessage {
	return nil
}
