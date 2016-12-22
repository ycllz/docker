package winlx

import (
	"runtime"
	"strings"
)

const osSplit = "-"

func EncodeOS(id, os string) string {
	return os + osSplit + id
}

func DecodeOS(id string) (string, string) {
	i := strings.Index(id, osSplit)
	if i == len(id)-1 {
		// This should never happen
		return "", ""
	} else if i == -1 {
		// Just return the runtime OS then
		return runtime.GOOS, id
	}
	return id[:i], id[i+1:]
}
