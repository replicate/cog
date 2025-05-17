package requirements

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/util/files"
)

const RequirementsFile = "requirements.txt"
const OverridesFile = "overrides.txt"

func GenerateRequirements(tmpDir string, path string, fileName string) (string, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	requirements := string(bs)

	// Check against the old requirements
	requirementsFile := filepath.Join(tmpDir, fileName)
	if err := files.WriteIfDifferent(requirementsFile, requirements); err != nil {
		return "", err
	}
	return requirementsFile, err
}

func CurrentRequirements(tmpDir string) (string, error) {
	requirementsFile := filepath.Join(tmpDir, RequirementsFile)
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
	defer fh.Close()

	// Use scanner to handle CRLF endings
	scanner := bufio.NewScanner(fh)
	scanner.Split(scanLinesWithContinuations)

	var requirements []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comment lines
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Remove any trailing comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}

		if line != "" {
			requirements = append(requirements, line)
		}
	}

	return requirements, scanner.Err()
}

// scanLinesWithContinuations is a modified version of bufio.ScanLines that
// also handles line continuations (lines ending with a backslash).
func scanLinesWithContinuations(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// If we're at EOF and there's no data, return nil
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	var line []byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			end := i
			if end > 0 && data[end-1] == '\r' {
				end--
			}
			// Add this segment to our accumulated line
			line = append(line, data[start:end]...)

			if len(line) > 0 && line[len(line)-1] == '\\' {
				// This is a continuation - remove the backslash and continue
				line = line[:len(line)-1]
				start = i + 1
				continue
			}

			if len(line) == 0 {
				continue
			}

			// Not a continuation, return the accumulated line
			return i + 1, line, nil
		}
	}

	// If we're at EOF, we have a final, non-terminated line
	if atEOF {
		if len(data) > start {
			line = append(line, data[start:]...)
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
		}
		return len(data), line, nil
	}

	// Need more data
	return 0, nil, nil
}
