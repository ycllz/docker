package winlx

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Windows Flags for posix semantics in CreateFile
const FILE_FLAG_POSIX_SEMANTICS = 0x01000000

// Contains all the illegal Windows char, but not Unix
// Note that # is actually legal, but we encode
//since it's used as special chars for our encoding.
const legalUnixChars = "#:\n\\"
const encodingPrefix = "#"
const osSplit = "-"

func toHex(x rune) rune {
	if x >= 0 && x <= 9 {
		return '0' + x
	}
	return 'A' + (x - 10)
}

func encodeChar(c rune) []rune {
	// convert from illegal char to hex code
	// so '<' to "#3C"

	d1 := (c >> 4) & 0xF
	d2 := c & 0xF

	r := []rune{'#', toHex(d1), toHex(d2)}
	return r
}

// FixUnixPath converts a unix path to a legal windows path.
func FixUnixPath(unixPath string) string {
	// Remove trailing slashes
	unixPath = strings.TrimRight(unixPath, "/")

	// Encode all the legal unix characters
	newString := []rune{}

	for _, c := range unixPath {
		if strings.ContainsRune(legalUnixChars, c) {
			newString = append(newString, encodeChar(c)...)
		} else {
			newString = append(newString, c)
		}
	}

	// Now, just replace the '/' with '\'
	return filepath.FromSlash(string(newString))
}

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
