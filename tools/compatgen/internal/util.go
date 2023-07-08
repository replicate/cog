package internal

import (
	"strings"
)

func split2(s string, sep string) (string, string) {
	parts := strings.SplitN(s, sep, 2)
	return parts[0], parts[1]
}
