package migrate

import "context"

type Migrator interface {
	Migrate(ctx context.Context, configFilename string) error
}
