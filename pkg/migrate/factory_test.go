package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMigrator(t *testing.T) {
	migrator, err := NewMigrator(MigrationV1, MigrationV1Fast, false)
	require.NoError(t, err)
	require.NotNil(t, migrator)
}
