package docker

type Command interface {
	Push(string) error
}
