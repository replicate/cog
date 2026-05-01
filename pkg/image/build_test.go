package image

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

var hasGit = (func() bool {
	_, err := exec.LookPath("git")
	return err == nil
})()

func gitRun(ctx context.Context, argv []string, t *testing.T) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	t.Cleanup(cancel)

	out, err := exec.CommandContext(ctx, "git", argv...).CombinedOutput()
	t.Logf("git output:\n%s", string(out))

	require.NoError(t, err)
}

func setupGitWorkTree(t *testing.T) string {
	ctx := t.Context()
	if !hasGit {
		t.Skip("no git executable available")
		return ""
	}

	r := require.New(t)

	tmp := filepath.Join(t.TempDir(), "wd")
	r.NoError(os.MkdirAll(tmp, 0o755))

	gitRun(ctx, []string{"init", tmp}, t)
	gitRun(ctx, []string{"-C", tmp, "config", "user.email", "cog@localhost"}, t)
	gitRun(ctx, []string{"-C", tmp, "config", "user.name", "Cog Tests"}, t)
	gitRun(ctx, []string{"-C", tmp, "commit", "--allow-empty", "-m", "walrus"}, t)
	gitRun(ctx, []string{"-C", tmp, "tag", "-a", "v0.0.1+walrus", "-m", "walrus time"}, t)

	return tmp
}

func TestIsGitWorkTree(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	r.False(isGitWorkTree(ctx, "/dev/null"))
	r.True(isGitWorkTree(ctx, setupGitWorkTree(t)))
}

func TestGitHead(t *testing.T) {
	t.Run("via github env", func(t *testing.T) {
		t.Setenv("GITHUB_SHA", "fafafaf")

		head, err := gitHead(t.Context(), "/dev/null")

		require.NoError(t, err)
		require.Equal(t, "fafafaf", head)
	})

	t.Run("via git", func(t *testing.T) {
		tmp := setupGitWorkTree(t)
		if tmp == "" {
			return
		}

		t.Setenv("GITHUB_SHA", "")

		head, err := gitHead(t.Context(), tmp)
		require.NoError(t, err)
		require.NotEqual(t, "", head)
	})

	t.Run("unavailable", func(t *testing.T) {
		t.Setenv("GITHUB_SHA", "")

		head, err := gitHead(t.Context(), "/dev/null")
		require.Error(t, err)
		require.Equal(t, "", head)
	})
}

func TestGitTag(t *testing.T) {
	t.Run("via github env", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "v0.0.1+manatee")

		tag, err := gitTag(t.Context(), "/dev/null")
		require.NoError(t, err)
		require.Equal(t, "v0.0.1+manatee", tag)
	})

	t.Run("via git", func(t *testing.T) {
		tmp := setupGitWorkTree(t)
		if tmp == "" {
			return
		}

		t.Setenv("GITHUB_REF_NAME", "")

		tag, err := gitTag(t.Context(), tmp)
		require.NoError(t, err)
		require.Equal(t, "v0.0.1+walrus", tag)
	})

	t.Run("unavailable", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "")

		tag, err := gitTag(t.Context(), "/dev/null")
		require.Error(t, err)
		require.Equal(t, "", tag)
	})
}

func TestUseStaticSchemaGen(t *testing.T) {
	// Helper to build a config with a specific SDK version.
	cfgWithSDK := func(version string) *config.Config {
		return &config.Config{
			Build: &config.Build{SDKVersion: version},
		}
	}
	noBuild := &config.Config{}

	tests := []struct {
		name     string
		cfg      *config.Config
		legacy   string // COG_LEGACY_SCHEMA value
		static   string // COG_STATIC_SCHEMA value (legacy opt-in, now a no-op)
		sdkWheel string // COG_SDK_WHEEL value
		want     bool
	}{
		// --- Default: static gen is on ---
		{
			name: "static by default (no env vars set)",
			cfg:  cfgWithSDK("0.18.0"),
			want: true,
		},
		{
			name: "static by default for unpinned SDK",
			cfg:  noBuild,
			want: true,
		},
		{
			name:     "static by default for new SDK via COG_SDK_WHEEL",
			cfg:      noBuild,
			sdkWheel: "pypi:0.18.0",
			want:     true,
		},

		// --- Legacy opt-out via COG_LEGACY_SCHEMA ---
		{
			name:   "legacy path when COG_LEGACY_SCHEMA=1",
			cfg:    cfgWithSDK("0.18.0"),
			legacy: "1",
			want:   false,
		},
		{
			name:   "legacy path when COG_LEGACY_SCHEMA=true",
			cfg:    cfgWithSDK("0.18.0"),
			legacy: "true",
			want:   false,
		},
		{
			name:   "legacy path when COG_LEGACY_SCHEMA=True (mixed case)",
			cfg:    cfgWithSDK("0.18.0"),
			legacy: "True",
			want:   false,
		},
		{
			name:   "legacy path when COG_LEGACY_SCHEMA=TRUE (upper case)",
			cfg:    cfgWithSDK("0.18.0"),
			legacy: "TRUE",
			want:   false,
		},
		{
			name:   "static path when COG_LEGACY_SCHEMA is empty string",
			cfg:    cfgWithSDK("0.18.0"),
			legacy: "",
			want:   true,
		},
		{
			name:   "static path when COG_LEGACY_SCHEMA=0",
			cfg:    cfgWithSDK("0.18.0"),
			legacy: "0",
			want:   true,
		},

		// --- SDK version gating ---
		{
			name: "legacy path for old pinned SDK (below 0.17.0)",
			cfg:  cfgWithSDK("0.16.12"),
			want: false,
		},
		{
			name: "legacy path for pre-release old SDK",
			cfg:  cfgWithSDK("0.16.0a1"),
			want: false,
		},
		{
			name: "static path for SDK 0.17.0 (min supported)",
			cfg:  cfgWithSDK("0.17.0"),
			want: true,
		},
		{
			name:     "legacy path for old SDK via COG_SDK_WHEEL",
			cfg:      noBuild,
			sdkWheel: "pypi:0.16.12",
			want:     false,
		},

		// --- Back-compat with old COG_STATIC_SCHEMA=1 flag (should be a no-op) ---
		{
			name:   "COG_STATIC_SCHEMA=1 is a no-op (static remains the default)",
			cfg:    cfgWithSDK("0.18.0"),
			static: "1",
			want:   true,
		},
		{
			name:   "COG_STATIC_SCHEMA=1 does not override COG_LEGACY_SCHEMA=1",
			cfg:    cfgWithSDK("0.18.0"),
			static: "1",
			legacy: "1",
			want:   false,
		},
		{
			name:   "COG_STATIC_SCHEMA=1 cannot force static on old pinned SDK",
			cfg:    cfgWithSDK("0.16.12"),
			static: "1",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Explicitly clear every env var the function consults before
			// applying the subtest-specific values. t.Setenv(key, "") is
			// equivalent to "unset" for our truthy-value checks, but being
			// explicit here makes the test robust against any future
			// additions to the struct where a field might be forgotten.
			t.Setenv("COG_LEGACY_SCHEMA", "")
			t.Setenv("COG_STATIC_SCHEMA", "")
			t.Setenv("COG_SDK_WHEEL", "")

			t.Setenv("COG_LEGACY_SCHEMA", tt.legacy)
			t.Setenv("COG_STATIC_SCHEMA", tt.static)
			t.Setenv("COG_SDK_WHEEL", tt.sdkWheel)

			got := useStaticSchemaGen(tt.cfg)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWriteRuntimeWeightsManifest(t *testing.T) {
	dir := t.TempDir()

	lock := &lockfile.WeightsLock{
		Version: lockfile.Version,
		Weights: []lockfile.WeightLockEntry{
			{
				Name:      "model-a",
				Target:    "/src/weights/a",
				SetDigest: "sha256:aaa111",
				Digest:    "sha256:manifest-a",
			},
			{
				Name:      "model-b",
				Target:    "/src/weights/b",
				SetDigest: "sha256:bbb222",
				Digest:    "sha256:manifest-b",
			},
		},
	}
	require.NoError(t, lock.Save(filepath.Join(dir, lockfile.WeightsLockFilename)))

	// writeRuntimeWeightsManifest writes to the CWD-relative bundledWeightsFile.
	t.Chdir(t.TempDir())

	require.NoError(t, writeRuntimeWeightsManifest(dir))

	data, err := os.ReadFile(bundledWeightsFile)
	require.NoError(t, err)

	var manifest lockfile.RuntimeWeightsManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	require.Len(t, manifest.Weights, 2)

	assert.Equal(t, "model-a", manifest.Weights[0].Name)
	assert.Equal(t, "/src/weights/a", manifest.Weights[0].Target)
	assert.Equal(t, "sha256:aaa111", manifest.Weights[0].SetDigest)

	assert.Equal(t, "model-b", manifest.Weights[1].Name)
	assert.Equal(t, "/src/weights/b", manifest.Weights[1].Target)
	assert.Equal(t, "sha256:bbb222", manifest.Weights[1].SetDigest)

	// Verify the JSON contains only the spec §3.3 fields (no lockfile extras).
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	var entries []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["weights"], &entries))
	for i, entry := range entries {
		keys := make([]string, 0, len(entry))
		for k := range entry {
			keys = append(keys, k)
		}
		assert.ElementsMatch(t, []string{"name", "target", "setDigest"}, keys,
			"entry %d must have exactly the spec §3.3 fields", i)
	}
}

func TestWriteRuntimeWeightsManifest_MissingLockfile(t *testing.T) {
	err := writeRuntimeWeightsManifest(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "managed weights configured but no lockfile found")
}

func TestCollectBundleFiles_SchemaOnly(t *testing.T) {
	t.Chdir(t.TempDir())

	files := collectBundleFiles([]byte(`{"openapi":"3.0.0"}`))
	assert.Equal(t, []string{bundledSchemaFile}, files)
}

func TestCollectBundleFiles_Nothing(t *testing.T) {
	t.Chdir(t.TempDir())

	files := collectBundleFiles(nil)
	assert.Empty(t, files)
}

func TestCollectBundleFiles_WithWeightsFile(t *testing.T) {
	t.Chdir(t.TempDir())

	require.NoError(t, os.MkdirAll(".cog", 0o755))
	require.NoError(t, os.WriteFile(bundledWeightsFile, []byte(`{"weights":[]}`), 0o644))

	files := collectBundleFiles([]byte(`{"openapi":"3.0.0"}`))
	assert.Equal(t, []string{bundledSchemaFile, bundledWeightsFile}, files)
}

func TestBundleDockerfile(t *testing.T) {
	df := bundleDockerfile("myimage:latest", []string{bundledSchemaFile, bundledWeightsFile})
	assert.Contains(t, df, "FROM myimage:latest")
	assert.Contains(t, df, "COPY .cog/openapi_schema.json .cog/")
	assert.Contains(t, df, "COPY .cog/weights.json .cog/")
}
