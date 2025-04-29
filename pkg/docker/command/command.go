package command

import "context"

type Command interface {
	Pull(ctx context.Context, ref string) error
	Push(ctx context.Context, ref string) error
	LoadUserInformation(ctx context.Context, registryHost string) (*UserInfo, error)
	CreateTarFile(ctx context.Context, ref string, tmpDir string, tarFile string, folder string) (string, error)
	CreateAptTarFile(ctx context.Context, tmpDir string, aptTarFile string, packages ...string) (string, error)
	Inspect(ctx context.Context, ref string) (*Manifest, error)
}
