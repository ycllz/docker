// +build !windows

package dockerfile

func defaultShellForPlatform(platform string) []string {
	return []string{"sh", "-c"}
}
