package errors

import (
	"errors"

	"github.com/replicate/cog/pkg/global"
)

var (
	ErrorBadRegistryURL = errors.New("The image URL must have 3 components in the format of " + global.ReplicateRegistryHost + "/your-username/your-model")
)
