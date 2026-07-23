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
	err := fs.WalkDir(playgroundUI, "playground", func(name string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() || (!strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css")) {
			return nil
		}
		contents, err := playgroundUI.ReadFile(name)
		require.NoError(t, err)
		patterns := []*regexp.Regexp{imports}
		if strings.HasSuffix(name, ".css") {
			patterns = []*regexp.Regexp{styles}
		}
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllSubmatch(contents, -1) {
				reference := string(match[1])
				if !strings.HasPrefix(reference, ".") && !strings.HasPrefix(reference, "/") {
					continue
				}
				target := path.Clean(path.Join(path.Dir(name), reference))
				if strings.HasPrefix(reference, "/") {
					target = path.Join("playground", reference)
				}
				_, err := fs.Stat(playgroundUI, target)
				require.NoError(t, err, "%s references missing asset %s", name, reference)
			}
		}
		return nil
	})
	require.NoError(t, err)
}
