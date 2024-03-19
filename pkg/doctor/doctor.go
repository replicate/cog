package doctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	w "github.com/replicate/cog/pkg/weights"
)

var problematicPrefixes = []string{".cog", ".git", "__pycache__"}

var suffixesToIgnore = []string{
	".py", ".ipynb", ".whl", // Python projects
	".jpg", ".jpeg", ".png", ".webp", ".svg", ".gif", ".avif", ".heic", // images
	".mp4", ".mov", ".avi", ".wmv", ".mkv", ".webm", // videos
	".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", // audio files
	".log", // logs
}

type FileWalker func(root string, walkFn filepath.WalkFunc) error

func CheckFiles() error {

	ignore, err := parseDockerignore()
	if err != nil {
		return err
	}

	problemDirs, weightFiles, err := walk(filepath.Walk, ignore)
	if err != nil {
		return err
	}

	if len(problemDirs) > 0 {
		fmt.Println("These directories can likely be excluded from your image:")
		for _, dir := range problemDirs {
			fmt.Printf("\t\033[31m%s\033[0m\n", dir)
		}
		fmt.Printf("\nYou can exclude them by adding them to your .dockerignore file.\n\n")
	}

	if len(weightFiles) > 0 {
		fmt.Println("These files are large and better excluded from your image:")
		for _, file := range weightFiles {
			fmt.Printf("\t\033[32m%s\033[0m\n", file)
		}
		fmt.Printf("\nYou can host them in Replicate's File Cache.\n\n")
	}

	return nil
}

const sizeThreshold = 20 * 1024 * 1024 // 20MB

func walk(fw FileWalker, ignore []string) (weights []string, problemDirs []string, e error) {
	err := fw(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		for _, pattern := range ignore {

			if strings.HasPrefix(path, strings.TrimSuffix(pattern, "/")) {
				return nil
			}
			match, err := doublestar.PathMatch(pattern, path)
			if err != nil {
				return err
			}
			if match {
				return nil
			}
		}

		// If it's a directory, we just check if it's "problematic"
		if info.IsDir() {
			for _, prefix := range problematicPrefixes {
				if strings.HasPrefix(info.Name(), prefix) {
					problemDirs = append(problemDirs, path)
				}
			}
			return nil
		}

		// Filter out files in "problematic" directories
		for _, prefix := range problematicPrefixes {
			if strings.HasPrefix(path, prefix) {
				return nil
			}
		}

		// Filter out "known" suffixes
		for _, suffix := range suffixesToIgnore {
			if strings.HasSuffix(path, suffix) {
				return nil
			}
		}

		// Filter out weights that are too small / not worth pget'ing
		if info.Size() < sizeThreshold {
			return nil
		}

		weights = append(weights, path)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// by sorting the files by levels, we can filter out directories that are prefixes of other directories
	// e.g. /a/b/ is a prefix of /a/b/c/, so we can filter out /a/b/c/
	w.SortFilesByLevels(weights)

	return problemDirs, weights, nil
}

func parseDockerignore() ([]string, error) {
	file, err := os.Open(".dockerignore")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 && line[0] != '#' {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
