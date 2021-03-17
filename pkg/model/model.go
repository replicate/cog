package model

type Target string

const (
	TargetDockerCPU = "docker-cpu"
	TargetDockerGPU = "docker-gpu"
)

type Model struct {
	ID        string
	Name      string
	Artifacts []*Artifact
	Config    *Config
}

type Artifact struct {
	Target Target
	URI    string
}

func (m *Model) ArtifactFor(target Target) (artifact *Artifact, ok bool) {
	for _, a := range m.Artifacts {
		if a.Target == target {
			return a, true
		}
	}
	return nil, false
}
