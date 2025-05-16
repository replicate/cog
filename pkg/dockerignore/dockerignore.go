package dockerignore

import (
	"bufio"
	"os"
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/replicate/cog/pkg/util/files"
)

const DockerIgnoreFilename = ".dockerignore"

func CreateMatcher(dir string) (*ignore.GitIgnore, error) {
	dockerIgnorePath := filepath.Join(dir, DockerIgnoreFilename)
	dockerIgnoreExists, err := files.Exists(dockerIgnorePath)
	if err != nil {
		return nil, err
	}
	if !dockerIgnoreExists {
		return nil, nil
	}

	patterns, err := readDockerIgnore(dockerIgnorePath)
	if err != nil {
		return nil, err
	}
	return ignore.CompileIgnoreLines(patterns...), nil
}

func Walk(root string, ignoreMatcher *ignore.GitIgnore, fn filepath.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// We ignore files ignored by .dockerignore
		if ignoreMatcher != nil && ignoreMatcher.MatchesPath(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() && info.Name() == ".cog" {
			return filepath.SkipDir
		}

		if info.Name() == DockerIgnoreFilename {
			return nil
		}

		return fn(path, info, err)
	})
}

func readDockerIgnore(dockerIgnorePath string) ([]string, error) {
	var patterns []string
	file, err := os.Open(dockerIgnorePath)
	if err != nil {
		return patterns, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}
