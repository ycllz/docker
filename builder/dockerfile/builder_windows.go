package dockerfile

func defaultShellForPlatform(platform string) []string {
	if platform == "linux" {
		return []string{"sh", "-c"}
	}
	return []string{"cmd", "/S", "/C"}
}
