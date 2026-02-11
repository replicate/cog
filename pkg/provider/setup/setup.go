// Package setup initializes the default provider registry
package setup

import (
	"sync"

	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/generic"
	"github.com/replicate/cog/pkg/provider/replicate"
)

var once sync.Once

// registerBuiltinProviders registers all built-in providers on the given registry.
// Providers are registered in priority order: Replicate first (more specific),
// then Generic as a fallback for any OCI registry.
func registerBuiltinProviders(reg *provider.Registry) {
	reg.Register(replicate.New())
	reg.Register(generic.New())
}

// NewRegistry creates a new provider registry with all built-in providers registered.
func NewRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	registerBuiltinProviders(reg)
	return reg
}

// Init initializes the default provider registry with all built-in providers.
// This function is idempotent - it only runs once even if called multiple times.
func Init() {
	once.Do(func() {
		registerBuiltinProviders(provider.DefaultRegistry())
	})
}
