package server

type Dockerfile struct {
	Cpu string `yaml:"cpu"`
	Gpu string `yaml:"gpu"`
}

type Config struct {
	Name string `yaml:"name"`
	Dockerfile Dockerfile `yaml:"dockerfile"`
	Model string `yaml:"model"`
}
