package dockerfile

import (
	"net/http"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

func NewGenerator(config *config.Config, dir string, buildFast bool, command command.Command, localImage bool, client registry.Client, requiresCog bool) (Generator, error) {
	if buildFast {
		console.Warn("experimental fast features, --x-fast / fast: true, are no longer supported. Use of flags/directives will error in a future release; falling back to standard build and generator.")
		buildFast = false
		if config != nil && config.Build != nil {
			cogRuntime := true
			config.Build.CogRuntime = &cogRuntime
		}
	}
	if buildFast {
		matrix, err := NewMonobaseMatrix(http.DefaultClient)
		if err != nil {
			return nil, err
		}
		return NewFastGenerator(config, dir, command, matrix, localImage)
	}
	return NewStandardGenerator(config, dir, command, client, requiresCog)
}
