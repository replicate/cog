package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/coglog"
)

func TestNewMigrator(t *testing.T) {
	logCtx := coglog.NewMigrateLogContext(true)
	migrator, err := NewMigrator(MigrationV1, MigrationV1Fast, false, logCtx)
	require.NoError(t, err)
	require.NotNil(t, migrator)
}
