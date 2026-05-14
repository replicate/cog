// Package modeltest provides shared test helpers for model-ref code.
package modeltest

import (
	"testing"

	"github.com/replicate/cog/pkg/model"
)

// ClearEnv isolates the test from the developer's shell by blanking
// every COG_MODEL* env var. Without this, a shell-exported COG_MODEL_REPO
// (or similar) would leak into the test process and silently change what
// ResolveModelRef sees. t.Setenv handles restoration at cleanup;
// ResolveModelRef treats "" as unset, so blanking is equivalent to
// unsetting.
func ClearEnv(t *testing.T) {
	t.Helper()
	t.Setenv(model.EnvModel, "")
	t.Setenv(model.EnvModelRegistry, "")
	t.Setenv(model.EnvModelRepo, "")
	t.Setenv(model.EnvModelTag, "")
}
