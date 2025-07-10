package stacks

import "github.com/replicate/cog/pkg/cogpack"

func init() {
	// Register all available stacks
	cogpack.RegisterStack(&PythonStack{})
}
