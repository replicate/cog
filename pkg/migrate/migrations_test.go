package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationToStr(t *testing.T) {
	migrationStr, err := MigrationToStr(MigrationV1)
	require.NoError(t, err)
	require.Equal(t, migrationStr, "v1")
}
