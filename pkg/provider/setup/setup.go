// Package setup initializes the default provider registry
package setup

import (
	"sync"

	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/generic"
	"github.com/replicate/cog/pkg/provider/replicate"
)

var once sync.Once

// Init initializes the default provider registry with all built-in providers
// This function is idempotent - it only runs once even if called multiple times
func Init() {
	once.Do(func() {
		registry := provider.DefaultRegistry()

		// Register Replicate provider first (more specific)
		registry.Register(replicate.New())

		// Register Generic provider last (fallback for any OCI registry)
		registry.Register(generic.New())
	})
}
