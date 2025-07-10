package core

import "context"

// Provider analyzes the project and mutates the given Plan by appending or
// changing Steps. Providers should be deterministic and idempotent.
type Provider interface {
	Name() string
	Configure(ctx context.Context, src *SourceInfo) error
	Detect(ctx context.Context, src *SourceInfo) (bool, error)
	Plan(ctx context.Context, src *SourceInfo, p *Plan) error
}

type DependencyResolver interface {
	Resolve(ctx context.Context, src *SourceInfo) ([]Dependency, error)
}
