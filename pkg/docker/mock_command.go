package docker

var PushError error = nil

type MockCommand struct{}

func NewMockCommand() *MockCommand {
	return &MockCommand{}
}

func (c *MockCommand) Push(image string) error {
	return PushError
}

func (c *MockCommand) LoadLoginToken(registryHost string) (string, error) {
	return "", nil
}
