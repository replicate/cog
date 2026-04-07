package validator

import (
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"regexp"
	"strings"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
)

// Result represents the result of a validation
type Result struct {
	Passed  bool
	Message string
}

// Func is a validation function type
type Func func(output string, expect manifest.Expectation) Result

var validators = map[string]Func{
	"exact":       validateExact,
	"contains":    validateContains,
	"regex":       validateRegex,
	"file_exists": validateFileExists,
	"json_match":  validateJSONMatch,
	"json_keys":   validateJSONKeys,
	"not_empty":   validateNotEmpty,
}

// Validate runs the appropriate validator for the expectation type
func Validate(output string, expect manifest.Expectation) Result {
	vtype := expect.Type
	if vtype == "" {
		vtype = "not_empty"
	}

	validator, ok := validators[vtype]
	if !ok {
		return Result{
			Passed:  false,
			Message: fmt.Sprintf("Unknown validation type: %q", vtype),
		}
	}

	return validator(output, expect)
}

func validateExact(output string, expect manifest.Expectation) Result {
	expected := fmt.Sprint(expect.Value)
	clean := strings.TrimSpace(output)
	if clean == expected {
		return Result{Passed: true, Message: "Exact match"}
	}
	return Result{
		Passed:  false,
		Message: fmt.Sprintf("Expected exact match:\n  expected: %q\n  got:      %q", expected, clean),
	}
}

func validateContains(output string, expect manifest.Expectation) Result {
	substring := fmt.Sprint(expect.Value)
	if strings.Contains(output, substring) {
		return Result{Passed: true, Message: fmt.Sprintf("Contains %q", substring)}
	}
	return Result{
		Passed:  false,
		Message: fmt.Sprintf("Expected output to contain %q, got:\n  %q", substring, output[:min(len(output), 200)]),
	}
}

func validateRegex(output string, expect manifest.Expectation) Result {
	pattern := expect.Pattern
	if pattern == "" {
		return Result{Passed: false, Message: "No regex pattern provided"}
	}
	matched, err := regexp.MatchString(pattern, output)
	if err != nil {
		return Result{Passed: false, Message: fmt.Sprintf("Invalid regex pattern: %v", err)}
	}
	if matched {
		return Result{Passed: true, Message: fmt.Sprintf("Matches pattern %q", pattern)}
	}
	return Result{
		Passed:  false,
		Message: fmt.Sprintf("Output does not match regex %q:\n  %q", pattern, output[:min(len(output), 200)]),
	}
}

func validateFileExists(output string, expect manifest.Expectation) Result {
	pathStr := strings.TrimSpace(output)
	pathStr = strings.Trim(pathStr, `"'`)

	// Handle URLs
	if strings.HasPrefix(pathStr, "http://") || strings.HasPrefix(pathStr, "https://") {
		return Result{Passed: true, Message: fmt.Sprintf("Output is a URL: %s", pathStr)}
	}

	// Check file exists
	if _, err := os.Stat(pathStr); err != nil {
		return Result{Passed: false, Message: fmt.Sprintf("Output file does not exist: %s", pathStr)}
	}

	// Check MIME type if specified
	if expect.Mime != "" {
		guessed := mime.TypeByExtension(pathStr)
		if guessed != expect.Mime {
			return Result{
				Passed:  false,
				Message: fmt.Sprintf("Expected MIME %s, got %s for %s", expect.Mime, guessed, pathStr),
			}
		}
	}

	return Result{Passed: true, Message: fmt.Sprintf("File exists: %s", pathStr)}
}

func validateJSONMatch(output string, expect manifest.Expectation) Result {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		return Result{
			Passed:  false,
			Message: fmt.Sprintf("Output is not valid JSON: %v\n  %q", err, output[:min(len(output), 200)]),
		}
	}

	match := expect.Match
	if match == nil {
		return Result{Passed: true, Message: "Valid JSON (no match criteria)"}
	}

	if !isSubset(match, parsed) {
		return Result{
			Passed:  false,
			Message: fmt.Sprintf("JSON subset mismatch:\n  expected subset: %v\n  got: %v", match, parsed),
		}
	}

	return Result{Passed: true, Message: "JSON subset match"}
}

func validateJSONKeys(output string, expect manifest.Expectation) Result {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		return Result{
			Passed:  false,
			Message: fmt.Sprintf("Output is not valid JSON: %v\n  %q", err, output[:min(len(output), 200)]),
		}
	}

	if len(expect.Keys) > 0 {
		var missing []string
		for _, key := range expect.Keys {
			if _, ok := parsed[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return Result{
				Passed:  false,
				Message: fmt.Sprintf("Missing keys: %v. Got: %v", missing, getKeys(parsed)),
			}
		}
		return Result{Passed: true, Message: fmt.Sprintf("JSON dict with required keys: %v", expect.Keys)}
	}

	if len(parsed) == 0 {
		return Result{Passed: false, Message: "Expected non-empty JSON object, got empty dict"}
	}

	return Result{Passed: true, Message: fmt.Sprintf("JSON dict with %d keys: %v", len(parsed), getKeys(parsed)[:min(5, len(parsed))])}
}

func validateNotEmpty(output string, expect manifest.Expectation) Result {
	if strings.TrimSpace(output) != "" {
		return Result{Passed: true, Message: "Output is non-empty"}
	}
	return Result{Passed: false, Message: "Output is empty"}
}

// isSubset checks if subset is recursively contained in superset
func isSubset(subset, superset map[string]any) bool {
	for key, subVal := range subset {
		superVal, ok := superset[key]
		if !ok {
			return false
		}

		// Recursively check nested maps
		subMap, subIsMap := subVal.(map[string]any)
		superMap, superIsMap := superVal.(map[string]any)
		if subIsMap && superIsMap {
			if !isSubset(subMap, superMap) {
				return false
			}
			continue
		}

		// Check arrays
		subArr, subIsArr := subVal.([]any)
		superArr, superIsArr := superVal.([]any)
		if subIsArr && superIsArr {
			if !isArraySubset(subArr, superArr) {
				return false
			}
			continue
		}

		// Direct comparison
		if subVal != superVal {
			return false
		}
	}
	return true
}

// isArraySubset checks if each element of subset is contained in superset
func isArraySubset(subset, superset []any) bool {
	for _, subItem := range subset {
		found := false
		for _, superItem := range superset {
			// Try map comparison first
			subMap, subIsMap := subItem.(map[string]any)
			superMap, superIsMap := superItem.(map[string]any)
			if subIsMap && superIsMap {
				if isSubset(subMap, superMap) {
					found = true
					break
				}
			} else if subItem == superItem {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
