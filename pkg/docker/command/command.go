package command

type Command interface {
	Pull(string) error
	Push(string) error
	LoadUserInformation(string) (*UserInfo, error)
	CreateTarFile(string, string, string, string) (string, error)
	CreateAptTarFile(string, string, ...string) (string, error)
	Inspect(string) (*Manifest, error)
}
