package layer

import (
	"io"

	"github.com/docker/distribution"
)

func (ls *layerStore) RegisterWithDescriptor(ts io.Reader, parent ChainID, descriptor distribution.Descriptor, imagePlatform ImagePlatform) (Layer, error) {
	return ls.registerWithDescriptor(ts, parent, descriptor, imagePlatform)
}
