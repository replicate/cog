package stacks

import "github.com/replicate/cog/pkg/cogpack/stacks/python"

func init() {
	// Register all available stacks
	RegisterStack(&python.PythonStack{})
}
