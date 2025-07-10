package core

type Result struct {
	Plan         *Plan
	Providers    []string
	Dependencies map[string]Dependency
}
