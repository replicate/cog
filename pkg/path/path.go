package path

import (
	go_path "path"
	"strconv"
	"strings"
)

func TrimExt(s string) string {
	return strings.TrimSuffix(s, go_path.Ext(s))
}

func IsExtInteger(ext string) bool {
	ext = strings.TrimPrefix(ext, ".")
	_, err := strconv.Atoi(ext)
	return err == nil
}
