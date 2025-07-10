package cogpack

import (
	"strconv"

	"github.com/replicate/cog/pkg/util"
)

// Enabled reports whether the experimental cogpack build system is enabled in
// the current process. It returns true when the COGPACK environment variable is
// set to any truthy value ("1", "true", "yes" – case-insensitive).
func Enabled() bool {
	return util.GetEnvOrDefault("COGPACK", false, strconv.ParseBool)
}
