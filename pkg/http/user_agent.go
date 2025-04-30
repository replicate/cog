package http

import (
	"fmt"
	"runtime"

	"github.com/replicate/cog/pkg/global"
)

func UserAgent() string {
	var platform string
	switch runtime.GOOS {
	case "linux":
		platform = "Linux"
	case "windows":
		platform = "Windows"
	case "darwin":
		platform = "macOS"
	default:
		platform = runtime.GOOS
	}

	return fmt.Sprintf("Cog/%s (%s)", global.Version, platform)
}
