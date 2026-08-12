package cli

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlaygroundAssetGraphComplete(t *testing.T) {
	imports := regexp.MustCompile(`(?:from\s*|import\s*)["']([^"']+)["']`)
	styles := regexp.MustCompile(`@import\s+(?:url\()?\s*["']([^"']+)["']`)
	htmlAssets := regexp.MustCompile(`(?:href|src)\s*=\s*["']([^"']+)["']`)
	err := fs.WalkDir(playgroundUI, "playground", func(name string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() || (!strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css") && !strings.HasSuffix(name, ".html")) {
			return nil
		}
		contents, err := playgroundUI.ReadFile(name)
		require.NoError(t, err)
		patterns := []*regexp.Regexp{imports}
		if strings.HasSuffix(name, ".css") {
			patterns = []*regexp.Regexp{styles}
		} else if strings.HasSuffix(name, ".html") {
			patterns = []*regexp.Regexp{htmlAssets}
		}
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllSubmatch(contents, -1) {
				reference := string(match[1])
				if !strings.HasPrefix(reference, ".") && !strings.HasPrefix(reference, "/") {
					continue
				}
				target := path.Clean(path.Join(path.Dir(name), reference))
				if rootReference, ok := strings.CutPrefix(reference, "/"); ok {
					target = path.Join("playground", rootReference)
				}
				_, err := fs.Stat(playgroundUI, target)
				require.NoError(t, err, "%s references missing asset %s", name, reference)
			}
		}
		return nil
	})
	require.NoError(t, err)
	licenses, err := playgroundUI.ReadFile("playground/THIRD_PARTY_LICENSES.md")
	require.NoError(t, err)
	require.NotEmpty(t, licenses)
	for _, dependency := range []string{"@cloudflare/kumo", "@codemirror/state", "ajv", "ajv-draft-04", "ajv-formats", "fast-uri"} {
		require.Contains(t, string(licenses), "## "+dependency+" -")
	}
	for _, host := range []string{"cdnjs.cloudflare.com", "esm.unpkg.com", "esm.sh"} {
		err := fs.WalkDir(playgroundUI, "playground", func(name string, entry fs.DirEntry, walkErr error) error {
			require.NoError(t, walkErr)
			if entry.IsDir() || (!strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css") && !strings.HasSuffix(name, ".html")) {
				return nil
			}
			contents, err := playgroundUI.ReadFile(name)
			require.NoError(t, err)
			require.NotContains(t, string(contents), host, "%s references %s", name, host)
			return nil
		})
		require.NoError(t, err)
	}
}
