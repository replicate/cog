package docker

type ImageBuilder interface {
	BuildAndPush(dir string, dockerfilePath string, name string) (fullImageTag string, err error)
}
