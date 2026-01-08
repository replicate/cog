package version

import (
	"embed"
	"strings"
)

//go:embed *
var embedFS embed.FS

func Version() string {
	bs, err := embedFS.ReadFile("version.txt")
	if err != nil {
		return "0.0.0+unknown"
	}
	return strings.TrimSpace(string(bs))
}
