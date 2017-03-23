// +build !windows

package layer

import (
	"runtime"
)

func (ls *layerStore) encodeOS(osVersion, id string) string {
	return id
}

func (ls *layerStore) decodeOS(id string) (string, string, error) {
	return runtime.GOOS, id, nil
}
