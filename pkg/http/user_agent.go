package http

import (
	"fmt"

	"github.com/replicate/cog/pkg/global"
)

func UserAgent() string {
	return fmt.Sprintf("Cog/%s", global.Version)
}
