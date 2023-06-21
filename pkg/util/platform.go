package util

// IsAppleSiliconMac returns whether the current machine is an Apple silicon computer, such as the MacBook Air with M1.
func IsAppleSiliconMac(goos string, goarch string) bool {
	return goos == "darwin" && goarch == "arm64"
}
