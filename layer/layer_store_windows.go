package layer

import (
	"fmt"
	"io"

	winlx "github.com/Microsoft/go-winlx"
	"github.com/docker/distribution"
)

func (ls *layerStore) encodeOS(osVersion, id string) string {
	if ls.driver.String() == "windowsfilter" && osVersion != "" {
		// Append the OS information infront of the string.
		return winlx.EncodeOS(osVersion, id)
	}
	return id
}

func (ls *layerStore) decodeOS(id string) (string, string, error) {
	if ls.driver.String() == "windowsfilter" {
		osVersion, realID, err := winlx.DecodeOS(id)
		fmt.Printf("CONVERTED id -> realID: %s -> %s\n", id, realID)
		return osVersion, realID, err
	}
	return "windows", id, nil
}

func (ls *layerStore) RegisterWithDescriptor(ts io.Reader, parent ChainID, descriptor distribution.Descriptor) (Layer, error) {
	return ls.registerWithDescriptor(ts, parent, descriptor)
}
