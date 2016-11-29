package ntfsposix

import (
	"path/filepath"
	"strings"
)

// Windows Flags for posix semantics in CreateFile
const FILE_FLAG_POSIX_SEMANTICS = 0x01000000

// Contains all the illegal Windows char, but not Unix
// Note that the # and ! are actually legal, but we encode
//since it's used as special chars for our encoding.
const legalUnixChars = "<>:\"\\|?*#!"
const encodingPrefix = "#"
const encodingCapPrefix = "!"

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
		} else if c >= 'A' && c <= 'Z' {
			newString = append(newString, '!', c)
		} else {
			newString = append(newString, c)
		}
	}

	// Now, just replace the '/' with '\'
	return filepath.FromSlash(string(newString))
}
