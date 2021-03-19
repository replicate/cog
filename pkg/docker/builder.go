package docker

type ImageBuilder interface {
	BuildAndPush(dir string, dockerfilePath string, name string, logWriter func(string)) (fullImageTag string, err error)
}
