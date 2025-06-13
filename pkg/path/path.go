package path

import (
	go_path "path"
	"strings"
)

func TrimExt(s string) string {
	return strings.TrimSuffix(s, go_path.Ext(s))
}
