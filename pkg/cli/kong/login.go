package kong

// LoginCmd implements `cog login`.
type LoginCmd struct {
	TokenStdin bool `help:"Pass login token on stdin" name:"token-stdin"`
}

func (c *LoginCmd) Run(g *Globals) error {
	return nil // TODO
}
