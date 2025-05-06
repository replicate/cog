package migrate

import (
	"errors"
	"fmt"
)

type Migration int

const (
	MigrationV1 Migration = iota
	MigrationV1Fast
)

func MigrationToStr(migration Migration) (string, error) {
	switch migration {
	case MigrationV1:
		return "v1", nil
	case MigrationV1Fast:
		return "v1fast", nil
	}
	return "", errors.New(fmt.Sprintf("Unrecognized Migration: %d", migration))
}
