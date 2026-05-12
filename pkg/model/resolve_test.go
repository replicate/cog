package model

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/global"
)

func TestGenerateTimestampTag(t *testing.T) {
	tag := GenerateTimestampTag()

	// Round-trip through the timestamp format to confirm the output
	// is a valid time at the expected precision.
	parsed, err := time.Parse("20060102T150405Z", tag)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC(), parsed, 5*time.Second)
}

func TestResolveModelRef_ConfigOnly_GeneratesTimestamp(t *testing.T) {
	clearModelEnv(t)

	got, err := ResolveModelRef("registry.example.com/user/model")
	require.NoError(t, err)
	require.Equal(t, "registry.example.com", got.Registry)
	require.Equal(t, "user/model", got.Repo)
	require.Regexp(t, `^\d{8}T\d{6}Z$`, got.Tag)
	require.Empty(t, got.Digest)
	require.Equal(t, "registry.example.com/user/model:"+got.Tag, got.String())
}

func TestResolveModelRef_PartialConfigPlusEnv(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModelRegistry, "staging.io")

	got, err := ResolveModelRef("user/model")
	require.NoError(t, err)
	require.Equal(t, "staging.io", got.Registry)
	require.Equal(t, "user/model", got.Repo)
	require.NotEmpty(t, got.Tag)
	require.Equal(t, "staging.io/user/model:"+got.Tag, got.String())
}

func TestResolveModelRef_FullEnvOverridesEverything(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModel, "staging.io/foo/bar:v3")
	// Locks in "full ref wins outright": values that would normally
	// fail validation are tolerated because we never look at them.
	t.Setenv(EnvModelRegistry, "not/a/host")
	t.Setenv(EnvModelRepo, "user/model:tag")
	t.Setenv(EnvModelTag, "cog-reserved")

	got, err := ResolveModelRef("INVALID UPPERCASE/with spaces")
	require.NoError(t, err)
	require.Equal(t, "staging.io", got.Registry)
	require.Equal(t, "foo/bar", got.Repo)
	require.Equal(t, "v3", got.Tag)
	require.Equal(t, "staging.io/foo/bar:v3", got.String())
}

func TestResolveModelRef_FullEnvNoTag_GeneratesTimestamp(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModel, "staging.io/foo/bar")

	got, err := ResolveModelRef("")
	require.NoError(t, err)
	require.Equal(t, "staging.io", got.Registry)
	require.Equal(t, "foo/bar", got.Repo)
	require.Regexp(t, `^\d{8}T\d{6}Z$`, got.Tag)
}

func TestResolveModelRef_FullEnvWithPortNoTag(t *testing.T) {
	// "host:port/repo" must not be misread as "repo:tag" — the
	// localhost-registry workflow depends on this.
	clearModelEnv(t)
	t.Setenv(EnvModel, "localhost:5000/foo/bar")

	got, err := ResolveModelRef("")
	require.NoError(t, err)
	require.Equal(t, "localhost:5000", got.Registry)
	require.Equal(t, "foo/bar", got.Repo)
	require.Regexp(t, `^\d{8}T\d{6}Z$`, got.Tag)
}

func TestResolveModelRef_FullEnvWithPortAndTag(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModel, "localhost:5000/foo/bar:v3")

	got, err := ResolveModelRef("")
	require.NoError(t, err)
	require.Equal(t, "localhost:5000", got.Registry)
	require.Equal(t, "foo/bar", got.Repo)
	require.Equal(t, "v3", got.Tag)
}

func TestResolveModelRef_FullEnvWithDigest(t *testing.T) {
	clearModelEnv(t)
	digest := "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	t.Setenv(EnvModel, "staging.io/foo/bar@"+digest)

	got, err := ResolveModelRef("")
	require.NoError(t, err)
	require.Equal(t, "staging.io", got.Registry)
	require.Equal(t, "foo/bar", got.Repo)
	require.Empty(t, got.Tag)
	require.Equal(t, digest, got.Digest)
	require.Equal(t, "staging.io/foo/bar@"+digest, got.String())
}

func TestResolveModelRef_TagEnvOverridesTimestamp(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModelTag, "pr-42")

	got, err := ResolveModelRef("registry.example.com/user/model")
	require.NoError(t, err)
	require.Equal(t, "pr-42", got.Tag)
	require.Equal(t, "registry.example.com/user/model:pr-42", got.String())
}

func TestResolveModelRef_RepoEnvOverridesConfig(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModelRepo, "different/repo")

	got, err := ResolveModelRef("registry.example.com/user/model")
	require.NoError(t, err)
	require.Equal(t, "registry.example.com", got.Registry)
	require.Equal(t, "different/repo", got.Repo)
}

func TestResolveModelRef_NoRefAnywhere(t *testing.T) {
	clearModelEnv(t)

	_, err := ResolveModelRef("")
	require.ErrorIs(t, err, ErrNoModelRef)
}

func TestResolveModelRef_OnlyRegistryEnv_StillNeedsRepo(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModelRegistry, "staging.io")

	_, err := ResolveModelRef("")
	require.ErrorIs(t, err, ErrNoModelRef)
}

func TestResolveModelRef_ReservedTagPrefix(t *testing.T) {
	tests := []string{
		"cog-image.foo",
		"cog-weight.bar",
		"cog-something",
	}
	for _, tag := range tests {
		t.Run(tag, func(t *testing.T) {
			clearModelEnv(t)
			t.Setenv(EnvModelTag, tag)

			_, err := ResolveModelRef("registry.example.com/user/model")
			require.Error(t, err)
			require.Contains(t, err.Error(), `reserved prefix`)
			require.Contains(t, err.Error(), `cog-`)
		})
	}
}

func TestResolveModelRef_InvalidRegistryEnv(t *testing.T) {
	tests := []struct {
		name string
		val  string
	}{
		{"with path", "not-a-host/with/path"},
		{"with tag", "host.example.com:tag"},
		{"with digest", "host.example.com@sha256:abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearModelEnv(t)
			t.Setenv(EnvModelRegistry, tc.val)

			_, err := ResolveModelRef("user/model")
			require.Error(t, err)
			require.Contains(t, err.Error(), EnvModelRegistry)
		})
	}
}

func TestResolveModelRef_InvalidRepoEnv(t *testing.T) {
	tests := []struct {
		name string
		val  string
	}{
		{"with tag", "user/model:v1"},
		{"with digest", "user/model@sha256:abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearModelEnv(t)
			t.Setenv(EnvModelRepo, tc.val)

			_, err := ResolveModelRef("")
			require.Error(t, err)
			require.Contains(t, err.Error(), EnvModelRepo)
		})
	}
}

func TestResolveModelRef_InvalidTagFormat(t *testing.T) {
	clearModelEnv(t)
	// Leading hyphen is disallowed by the OCI tag regex.
	t.Setenv(EnvModelTag, "-bad-tag")

	_, err := ResolveModelRef("registry.example.com/user/model")
	require.Error(t, err)
	require.Contains(t, err.Error(), EnvModelTag)
}

func TestResolveModelRef_InvalidConfigModel(t *testing.T) {
	clearModelEnv(t)

	_, err := ResolveModelRef("INVALID UPPERCASE/with spaces")
	require.Error(t, err)
	require.Contains(t, err.Error(), "'model' in cog.yaml")
}

func TestResolveModelRef_BareConfigPath_InheritsGCRDefault(t *testing.T) {
	clearModelEnv(t)
	// go-containerregistry fills in Docker Hub when the config has no
	// explicit host. Cog's own Replicate-host fallback only applies
	// when configModel is empty (see RepoEnvOnly_UsesReplicateDefault).
	got, err := ResolveModelRef("user/model")
	require.NoError(t, err)
	require.Equal(t, "index.docker.io", got.Registry)
	require.Equal(t, "user/model", got.Repo)
}

func TestResolveModelRef_RepoEnvOnly_UsesReplicateDefault(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(EnvModelRepo, "user/model")

	got, err := ResolveModelRef("")
	require.NoError(t, err)
	require.Equal(t, global.ReplicateRegistryHost, got.Registry)
	require.Equal(t, "user/model", got.Repo)
}

func TestResolveModelRef_TimestampTagShape(t *testing.T) {
	clearModelEnv(t)

	got, err := ResolveModelRef("registry.example.com/user/model")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(got.Tag, "Z"))
	_, err = time.Parse("20060102T150405Z", got.Tag)
	require.NoError(t, err)
}

// clearModelEnv isolates the test from the developer's shell by
// blanking every COG_MODEL* env var. Without this, a shell-exported
// COG_MODEL_REPO (or similar) would leak into the test process and
// silently change what ResolveModelRef sees. t.Setenv handles
// restoration at cleanup; ResolveModelRef treats "" as unset, so
// blanking is equivalent to unsetting.
func clearModelEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvModel, "")
	t.Setenv(EnvModelRegistry, "")
	t.Setenv(EnvModelRepo, "")
	t.Setenv(EnvModelTag, "")
}
