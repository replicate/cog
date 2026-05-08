package image

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dotcog"
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

func TestBuildCodeDoesNotReferenceLegacyRuntimeSchemaGeneration(t *testing.T) {
	data, err := os.ReadFile("build.go")
	require.NoError(t, err)

	buildSource := string(data)
	for _, legacyReference := range []string{
		"COG_LEGACY_SCHEMA",
		"GenerateOpenAPISchema",
		"legacy runtime schema",
		"runtime path",
	} {
		assert.NotContains(t, strings.ToLower(buildSource), strings.ToLower(legacyReference))
	}
}

func TestValidateStaticSchemaSDKVersion(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		sdkWheel string
		wantErr  string
	}{
		{
			name:    "allows unpinned SDK",
			cfg:     &config.Config{},
			wantErr: "",
		},
		{
			name: "allows minimum SDK from config",
			cfg: &config.Config{
				Build: &config.Build{SDKVersion: "0.17.0"},
			},
			wantErr: "",
		},
		{
			name: "rejects old SDK from config",
			cfg: &config.Config{
				Build: &config.Build{SDKVersion: "0.16.12"},
			},
			wantErr: "SDK version 0.16.12 is not supported by static schema generation",
		},
		{
			name:     "rejects old SDK from env wheel",
			cfg:      &config.Config{},
			sdkWheel: "pypi:0.16.12",
			wantErr:  "SDK version 0.16.12 is not supported by static schema generation",
		},
		{
			name: "env wheel takes precedence over config",
			cfg: &config.Config{
				Build: &config.Build{SDKVersion: "0.16.12"},
			},
			sdkWheel: "pypi:0.17.0",
			wantErr:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("COG_SDK_WHEEL", tt.sdkWheel)

			err := validateStaticSchemaSDKVersion(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
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

	buildDir := t.TempDir()
	weightsFile := filepath.Join(buildDir, "weights.json")

	require.NoError(t, writeRuntimeWeightsManifest(dir, weightsFile))

	data, err := os.ReadFile(weightsFile)
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
	err := writeRuntimeWeightsManifest(t.TempDir(), filepath.Join(t.TempDir(), "weights.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "managed weights configured but no lockfile found")
}

func testBuildPaths(t *testing.T) buildPaths {
	t.Helper()
	buildDir := t.TempDir()
	return buildPaths{
		buildDir:        buildDir,
		schemaFile:      filepath.Join(buildDir, "openapi_schema.json"),
		weightsFile:     filepath.Join(buildDir, "weights.json"),
		weightsManifest: filepath.Join(buildDir, "weights_manifest.json"),
	}
}

func TestCollectBundleFiles_SchemaOnly(t *testing.T) {
	bp := testBuildPaths(t)
	files := collectBundleFiles([]byte(`{"openapi":"3.0.0"}`), &bp)
	assert.Equal(t, []string{bp.schemaFile}, files)
}

func TestCollectBundleFiles_Nothing(t *testing.T) {
	bp := testBuildPaths(t)
	files := collectBundleFiles(nil, &bp)
	assert.Empty(t, files)
}

func TestCollectBundleFiles_WithWeightsFile(t *testing.T) {
	bp := testBuildPaths(t)
	require.NoError(t, os.WriteFile(bp.weightsFile, []byte(`{"weights":[]}`), 0o644))

	files := collectBundleFiles([]byte(`{"openapi":"3.0.0"}`), &bp)
	assert.Equal(t, []string{bp.schemaFile, bp.weightsFile}, files)
}

func TestBundleDockerfile(t *testing.T) {
	bp := testBuildPaths(t)
	df := bundleDockerfile("myimage:latest", []string{bp.schemaFile, bp.weightsFile})
	assert.Contains(t, df, "FROM myimage:latest")
	assert.Contains(t, df, "COPY --from=cog_build openapi_schema.json "+dotcog.Name+"/")
	assert.Contains(t, df, "COPY --from=cog_build weights.json "+dotcog.Name+"/")
}
