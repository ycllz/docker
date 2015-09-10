// +build linux freebsd

package store

import "strings"

// normaliseVolumeName is a platform specific function to normalise the name
// of a volume. This is a no-op on Unix-like platforms
func normaliseVolumeName(name string) string {
	return strings.ToLower(name)
}
