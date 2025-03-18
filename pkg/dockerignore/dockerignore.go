package dockerignore

import (
	"bufio"
	"os"
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/replicate/cog/pkg/util/files"
)

func CreateMatcher(dir string) (*ignore.GitIgnore, error) {
	dockerIgnorePath := filepath.Join(dir, ".dockerignore")
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
