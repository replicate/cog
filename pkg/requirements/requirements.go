package requirements

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/util/files"
)

const REQUIREMENTS_FILE = "requirements.txt"

func GenerateRequirements(tmpDir string, path string) (string, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	requirements := string(bs)

	// Check against the old requirements
	requirementsFile := filepath.Join(tmpDir, REQUIREMENTS_FILE)
	if err := files.WriteIfDifferent(requirementsFile, requirements); err != nil {
		return "", err
	}
	return requirementsFile, err
}

func CurrentRequirements(tmpDir string) (string, error) {
	requirementsFile := filepath.Join(tmpDir, REQUIREMENTS_FILE)
	_, err := os.Stat(requirementsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return requirementsFile, nil
}

func ReadRequirements(path string) ([]string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// Use scanner to handle CRLF endings
	scanner := bufio.NewScanner(fh)
	scanner.Split(scanLinesWithContinuations)
	requirements := []string{}
	for scanner.Scan() {
		requirementsText := strings.TrimSpace(scanner.Text())
		if len(requirementsText) == 0 {
			continue
		}
		requirements = append(requirements, scanner.Text())
	}
	return requirements, nil
}

func scanLinesWithContinuations(data []byte, atEOF bool) (advance int, token []byte, err error) {
	advance = 0
	token = nil
	err = nil
	inHash := false
	for {
		if atEOF || len(data) == 0 {
			break
		}
		if token == nil {
			token = []byte{}
		}
		if data[advance] == '#' {
			inHash = true
		}
		if data[advance] == '\n' {
			if len(token) > 0 && token[len(token)-1] == '\\' {
				if !inHash {
					token = token[:len(token)-1]
				}
			} else {
				advance++
				break
			}
		} else {
			if !inHash {
				token = append(token, data[advance])
			}
		}
		advance++
		if advance == len(data) {
			break
		}
	}
	return advance, token, err
}
