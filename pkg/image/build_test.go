package image

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var hasGit = (func() bool {
	_, err := exec.LookPath("git")
	return err == nil
})()

func gitRun(argv []string, t *testing.T) {
	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Second)
	t.Cleanup(cancel)

	out, err := exec.CommandContext(ctx, "git", argv...).CombinedOutput()
	t.Logf("git output:\n%s", string(out))

	require.NoError(t, err)
}

func setupGitWorkTree(t *testing.T) string {
	if !hasGit {
		t.Skip("no git executable available")
		return ""
	}

	r := require.New(t)

	tmp := filepath.Join(t.TempDir(), "wd")
	r.NoError(os.MkdirAll(tmp, 0o755))

	gitRun([]string{"init", tmp}, t)
	gitRun([]string{"-C", tmp, "config", "user.email", "cog@localhost"}, t)
	gitRun([]string{"-C", tmp, "config", "user.name", "Cog Tests"}, t)
	gitRun([]string{"-C", tmp, "commit", "--allow-empty", "-m", "walrus"}, t)
	gitRun([]string{"-C", tmp, "tag", "-a", "v0.0.1+walrus", "-m", "walrus time"}, t)

	return tmp
}

func TestIsGitWorkTree(t *testing.T) {
	r := require.New(t)

	r.False(isGitWorkTree("/dev/null"))
	r.True(isGitWorkTree(setupGitWorkTree(t)))
}

func TestGitHead(t *testing.T) {
	t.Run("via github env", func(t *testing.T) {
		t.Setenv("GITHUB_SHA", "fafafaf")

		head, err := gitHead("/dev/null")

		require.NoError(t, err)
		require.Equal(t, "fafafaf", head)
	})

	t.Run("via git", func(t *testing.T) {
		tmp := setupGitWorkTree(t)
		if tmp == "" {
			return
		}

		t.Setenv("GITHUB_SHA", "")

		head, err := gitHead(tmp)
		require.NoError(t, err)
		require.NotEqual(t, "", head)
	})

	t.Run("unavailable", func(t *testing.T) {
		t.Setenv("GITHUB_SHA", "")

		head, err := gitHead("/dev/null")
		require.Error(t, err)
		require.Equal(t, "", head)
	})
}

func TestGitTag(t *testing.T) {
	t.Run("via github env", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "v0.0.1+manatee")

		tag, err := gitTag("/dev/null")
		require.NoError(t, err)
		require.Equal(t, "v0.0.1+manatee", tag)
	})

	t.Run("via git", func(t *testing.T) {
		tmp := setupGitWorkTree(t)
		if tmp == "" {
			return
		}

		t.Setenv("GITHUB_REF_NAME", "")

		tag, err := gitTag(tmp)
		require.NoError(t, err)
		require.Equal(t, "v0.0.1+walrus", tag)
	})

	t.Run("unavailable", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "")

		tag, err := gitTag("/dev/null")
		require.Error(t, err)
		require.Equal(t, "", tag)
	})
}
