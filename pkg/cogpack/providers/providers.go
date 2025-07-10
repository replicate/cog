package providers

import (
	"github.com/replicate/cog/pkg/cogpack/core"
	"github.com/replicate/cog/pkg/cogpack/providers/python"
)

func Providers() []core.Provider {
	return []core.Provider{
		&BaseImageProvider{},
		&APTProvider{},
		&python.PythonProvider{},
	}
}
