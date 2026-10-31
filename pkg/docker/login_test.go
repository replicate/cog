package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAuthToCredentialsStorePassNotInitialized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test credential helper is a shell script")
	}

	binDir := t.TempDir()
	helperPath := filepath.Join(binDir, "docker-credential-test")
	helper := `#!/bin/sh
cat >/dev/null
echo "pass not initialized: exit status 1" >&2
exit 1
`
	require.NoError(t, os.WriteFile(helperPath, []byte(helper), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := saveAuthToCredentialsStore(t.Context(), "test", "r8.im", "test-user", "secret-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "`pass` is not initialized")
	assert.Contains(t, err.Error(), "gpg --generate-key")
	assert.Contains(t, err.Error(), "pass init <gpg-id>")
	assert.NotContains(t, err.Error(), "secret-token")
}
