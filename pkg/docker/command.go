package docker

type Command interface {
	Push(string) error
	LoadLoginToken(string) (string, error)
	CreateTarFile(string, string, string, string) (string, error)
	CreateAptTarFile(string, string, ...string) (string, error)
}

type CredentialHelperInput struct {
	Username  string
	Secret    string
	ServerURL string
}
