package image

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupGitWorkTree(t *testing.T) string {
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
	r := require.New(t)
	tmp := setupGitWorkTree(t)

	head, err := gitHead(tmp)
	r.NoError(err)
	r.NotEqual("", head)

	head, err = gitHead("/dev/null")
	r.Error(err)
	r.Equal("", head)

	t.Setenv("GITHUB_SHA", "fafafaf")

	head, err = gitHead("/dev/null")
	r.NoError(err)
	r.Equal("fafafaf", head)
}

func TestGitTag(t *testing.T) {
	r := require.New(t)
	tmp := setupGitWorkTree(t)

	tag, err := gitTag(tmp)
	r.NoError(err)
	r.Equal("v0.0.1+walrus", tag)

	tag, err = gitTag("/dev/null")
	r.Error(err)
	r.Equal("", tag)

	t.Setenv("GITHUB_REF_NAME", "v0.0.1+manatee")

	tag, err = gitTag("/dev/null")
	r.NoError(err)
	r.Equal("v0.0.1+manatee", tag)
}
