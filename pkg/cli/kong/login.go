package kong

import (
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/util/console"
)

// LoginCmd implements `cog login`.
type LoginCmd struct {
	TokenStdin bool `help:"Pass login token on stdin instead of opening a browser" name:"token-stdin"`
}

func (c *LoginCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	setup.Init()

	registryHost := global.ReplicateRegistryHost

	p := provider.DefaultRegistry().ForHost(registryHost)
	if p == nil {
		console.Warnf("No provider found for registry '%s'.", registryHost)
		console.Infof("Please use 'docker login %s' to authenticate.", registryHost)
		return nil
	}

	return p.Login(ctx, provider.LoginOptions{
		TokenStdin: c.TokenStdin,
		Host:       registryHost,
	})
}
