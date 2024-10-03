package image

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

var hasGit = (func() bool {
	_, err := exec.LookPath("git")
	return err == nil
})()

func setupGitWorkTree(t *testing.T) string {
	if !hasGit {
		t.Skip("no git executable available")
		return ""
	}

	r := require.New(t)

	tmp := t.TempDir()

	r.NoError(exec.Command("git", "init", tmp).Run())
	r.NoError(exec.Command("git", "-C", tmp, "commit", "--allow-empty", "-m", "walrus").Run())
	r.NoError(exec.Command("git", "-C", tmp, "tag", "-a", "v0.0.1+walrus", "-m", "walrus time").Run())

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

		head, err := gitHead(tmp)
		require.NoError(t, err)
		require.NotEqual(t, "", head)
	})

	t.Run("unavailable", func(t *testing.T) {
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

		tag, err := gitTag(tmp)
		require.NoError(t, err)
		require.Equal(t, "v0.0.1+walrus", tag)
	})

	t.Run("unavailable", func(t *testing.T) {
		tag, err := gitTag("/dev/null")
		require.Error(t, err)
		require.Equal(t, "", tag)
	})
}
