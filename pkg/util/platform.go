package util

func IsM1Mac(goos string, goarch string) bool {
	return goos == "darwin" && goarch == "arm64"
}
