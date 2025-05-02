package migrate

import (
	"errors"
	"fmt"
)

func NewMigrator(from Migration, to Migration, interactive bool) (Migrator, error) {
	if from == MigrationV1 && to == MigrationV1Fast {
		return NewMigratorV1ToV1Fast(interactive), nil
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
