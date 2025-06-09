package migrate

import (
	"errors"
	"fmt"

	"github.com/replicate/cog/pkg/coglog"
)

func NewMigrator(from Migration, to Migration, interactive bool, logCtx *coglog.MigrateLogContext) (Migrator, error) {
	if from == MigrationV1 && to == MigrationV1Fast {
		return NewMigratorV1ToV1Fast(interactive, logCtx), nil
	}
	fromStr, err := MigrationToStr(from)
	if err != nil {
		return nil, err
	}
	toStr, err := MigrationToStr(to)
	if err != nil {
		return nil, err
	}
	return nil, errors.New(fmt.Sprintf("Unable to find a migrator from %s to %s.", fromStr, toStr))
}
