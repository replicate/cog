package core

type Dependency struct {
	Name             string
	Provider         string
	RequestedVersion string
	ResolvedVersion  string
	Source           string
}
