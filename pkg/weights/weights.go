package weights

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var prefixesToIgnore = []string{".cog", ".git", "__pycache__"}

var suffixesToIgnore = []string{
	".py", ".ipynb", ".whl", // Python projects
	".jpg", ".jpeg", ".png", ".webp", ".svg", ".gif", ".avif", ".heic", // images
	".mp4", ".mov", ".avi", ".wmv", ".mkv", ".webm", // videos
	".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", // audio files
	".log", // logs
}

// FileWalker is a function type that walks the file tree rooted at root, calling walkFn for each file or directory in the tree, including root.
type FileWalker func(root string, walkFn filepath.WalkFunc) error

func FindWeights(fw FileWalker) ([]string, []string, error) {
	var files []string
	var codeFiles []string
	err := fw(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if isCodeFile(path) {
			codeFiles = append(codeFiles, path)
			return nil
		}

		if info.Size() < sizeThreshold {
			return nil
		}
		if isNonModelFiles(path) {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// by sorting the files by levels, we can filter out directories that are prefixes of other directories
	// e.g. /a/b/ is a prefix of /a/b/c/, so we can filter out /a/b/c/
	sortFilesByLevels(files)

	dirs, rootFiles := getDirsAndRootfiles(files)
	dirs = filterDirsContainingCode(dirs, codeFiles)

	return dirs, rootFiles, nil
}

func isNonModelFiles(path string) bool {
	for _, prefix := range prefixesToIgnore {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, suffix := range suffixesToIgnore {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

const sizeThreshold = 10 * 1024 * 1024 // 10MB

func sortFilesByLevels(files []string) {
	sort.Slice(files, func(i, j int) bool {
		list1 := strings.Split(files[i], "/")
		list2 := strings.Split(files[j], "/")
		if len(list1) != len(list2) {
			return len(list1) < len(list2)
		}
		for k := range list1 {
			if list1[k] != list2[k] {
				return list1[k] < list2[k]
			}
		}
		return false
	})
}

// isCodeFile detects if a given path is a code file based on whether the file path ends with ".py" or ".ipynb"
func isCodeFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".py" || ext == ".ipynb"
}

// filterDirsContainingCode filters out directories that contain code files.
// If a dir is a prefix for any given codeFiles, it will be filtered out.
func filterDirsContainingCode(dirs []string, codeFiles []string) []string {
	filteredDirs := make([]string, 0, len(dirs))

	// Filter out directories that are prefixes of code directories
	for _, dir := range dirs {
		isPrefix := false
		for _, codeFile := range codeFiles {
			if strings.HasPrefix(codeFile, dir) {
				isPrefix = true
				break
			}
		}
		if !isPrefix {
			filteredDirs = append(filteredDirs, dir)
		}
	}

	return filteredDirs
}

func getDirsAndRootfiles(files []string) ([]string, []string) {
	// get all the directories that contain model weights files
	// remove sub-directories if their parent directory is already in the list
	var dirs []string

	// for large model files in root directory, we should not add the "." to dirs
	var rootFiles []string
	for _, f := range files {
		dir := filepath.Dir(f)
		if dir == "." || dir == "/" {
			rootFiles = append(rootFiles, f)
			continue
		}

		if hasParent(dir, dirs) {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs, rootFiles
}

func hasParent(dir string, dirs []string) bool {
	for _, d := range dirs {
		parent := d + string(filepath.Separator)
		child := dir + string(filepath.Separator)
		if strings.HasPrefix(child, parent) {
			return true
		}

	}
	return false
}
