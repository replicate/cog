package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

// TestResult represents a single test case result
type TestResult struct {
	Description string  `json:"description"`
	Passed      bool    `json:"passed"`
	Message     string  `json:"message"`
	DurationSec float64 `json:"duration_s"`
}

// ModelResult represents results for a single model
type ModelResult struct {
	Name          string       `json:"name"`
	Passed        bool         `json:"passed"`
	Skipped       bool         `json:"skipped,omitempty"`
	SkipReason    string       `json:"skip_reason,omitempty"`
	GPU           bool         `json:"gpu"`
	Error         string       `json:"error,omitempty"`
	BuildDuration float64      `json:"build_duration_s"`
	TestResults   []TestResult `json:"tests,omitempty"`
	TrainResults  []TestResult `json:"train_tests,omitempty"`
}

// SchemaCompareResult represents schema comparison results
type SchemaCompareResult struct {
	Name         string  `json:"name"`
	Passed       bool    `json:"passed"`
	Error        string  `json:"error,omitempty"`
	Diff         string  `json:"diff,omitempty"`
	StaticBuild  float64 `json:"static_build_s"`
	RuntimeBuild float64 `json:"runtime_build_s"`
}

// ConsoleReport prints a colored summary to the console
func ConsoleReport(results []ModelResult, sdkVersion, cogVersion string) {
	parts := []string{}
	if cogVersion != "" {
		parts = append(parts, fmt.Sprintf("CLI %s", cogVersion))
	}
	if sdkVersion != "" {
		parts = append(parts, fmt.Sprintf("SDK %s", sdkVersion))
	}

	header := "Cog Compatibility Report"
	if len(parts) > 0 {
		header = fmt.Sprintf("Cog Compatibility Report (%s)", strings.Join(parts, " / "))
	}

	fmt.Printf("\n%s\n", header)
	fmt.Printf("%s\n\n", strings.Repeat("=", len(header)))

	passed, failed, skipped := 0, 0, 0

	for _, r := range results {
		if r.Skipped {
			writeStatus("-", r.Name, r.SkipReason, r.GPU)
			skipped++
			continue
		}

		if r.Error != "" {
			// Print the summary line with just the first line of the error
			firstLine := r.Error
			if idx := strings.Index(firstLine, "\n"); idx != -1 {
				firstLine = firstLine[:idx]
			}
			writeStatus("x", r.Name, firstLine, r.GPU)
			// Print full error details indented below
			for line := range strings.SplitSeq(r.Error, "\n") {
				if line != "" {
					fmt.Printf("    %s\n", line)
				}
			}
			failed++
			continue
		}

		allTests := make([]TestResult, 0, len(r.TestResults)+len(r.TrainResults))
		allTests = append(allTests, r.TestResults...)
		allTests = append(allTests, r.TrainResults...)
		if r.Passed {
			timing := fmt.Sprintf("(%.1fs build", r.BuildDuration)
			if len(allTests) > 0 {
				totalPredict := 0.0
				for _, t := range allTests {
					totalPredict += t.DurationSec
				}
				timing += fmt.Sprintf(", %.1fs predict)", totalPredict)
			} else {
				timing += ")"
			}
			writeStatus("+", r.Name, timing, r.GPU)
			passed++
		} else {
			failures := []TestResult{}
			for _, t := range allTests {
				if !t.Passed {
					failures = append(failures, t)
				}
			}
			msg := fmt.Sprintf("%d test(s) failed", len(failures))
			if len(failures) > 0 {
				// Use just the first line of the first failure for the summary
				firstLine := failures[0].Message
				if idx := strings.Index(firstLine, "\n"); idx != -1 {
					firstLine = firstLine[:idx]
				}
				msg += fmt.Sprintf(": %s", firstLine)
			}
			writeStatus("x", r.Name, msg, r.GPU)
			failed++

			// Print individual failures with full output
			for _, t := range failures {
				fmt.Printf("    x %s:\n", t.Description)
				for line := range strings.SplitSeq(t.Message, "\n") {
					fmt.Printf("      %s\n", line)
				}
			}
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("-", 40))
	total := passed + failed + skipped
	fmt.Printf("%d/%d passed", passed, total)
	if skipped > 0 {
		fmt.Printf(", %d skipped", skipped)
	}
	if failed > 0 {
		fmt.Printf(", %d FAILED", failed)
	}
	fmt.Println()
	fmt.Println()
}

// JSONReport generates a JSON report
func JSONReport(results []ModelResult, sdkVersion, cogVersion string) map[string]any {
	models := []map[string]any{}
	for _, r := range results {
		entry := map[string]any{
			"name":             r.Name,
			"passed":           r.Passed,
			"skipped":          r.Skipped,
			"gpu":              r.GPU,
			"build_duration_s": round(r.BuildDuration, 2),
		}
		if r.Skipped {
			entry["skip_reason"] = r.SkipReason
		}
		if r.Error != "" {
			entry["error"] = r.Error
		}
		if len(r.TestResults) > 0 {
			tests := []map[string]any{}
			for _, t := range r.TestResults {
				tests = append(tests, map[string]any{
					"description": t.Description,
					"passed":      t.Passed,
					"message":     t.Message,
					"duration_s":  round(t.DurationSec, 2),
				})
			}
			entry["tests"] = tests
		}
		if len(r.TrainResults) > 0 {
			trainTests := []map[string]any{}
			for _, t := range r.TrainResults {
				trainTests = append(trainTests, map[string]any{
					"description": t.Description,
					"passed":      t.Passed,
					"message":     t.Message,
					"duration_s":  round(t.DurationSec, 2),
				})
			}
			entry["train_tests"] = trainTests
		}
		models = append(models, entry)
	}

	total := len(results)
	passed := 0
	failed := 0
	skipped := 0
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Passed:
			passed++
		default:
			failed++
		}
	}

	return map[string]any{
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"cog_version": cogVersion,
		"sdk_version": sdkVersion,
		"summary": map[string]int{
			"total":   total,
			"passed":  passed,
			"failed":  failed,
			"skipped": skipped,
		},
		"models": models,
	}
}

// WriteJSONReport writes a JSON report to a file or stdout
func WriteJSONReport(results []ModelResult, sdkVersion, cogVersion string, w io.Writer) error {
	report := JSONReport(results, sdkVersion, cogVersion)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// SchemaCompareConsoleReport prints schema comparison results
func SchemaCompareConsoleReport(results []SchemaCompareResult, cogVersion string) {
	header := "Schema Comparison Report"
	if cogVersion != "" {
		header = fmt.Sprintf("Schema Comparison Report (CLI %s)", cogVersion)
	}

	fmt.Printf("\n%s\n", header)
	fmt.Printf("%s\n\n", strings.Repeat("=", len(header)))

	passed, failed := 0, 0

	for _, r := range results {
		if r.Error != "" {
			firstLine := r.Error
			if idx := strings.Index(firstLine, "\n"); idx != -1 {
				firstLine = firstLine[:idx]
			}
			writeStatus("x", r.Name, firstLine, false)
			for line := range strings.SplitSeq(r.Error, "\n") {
				if line != "" {
					fmt.Printf("    %s\n", line)
				}
			}
			failed++
			continue
		}

		if r.Passed {
			timing := fmt.Sprintf("(static %.1fs, runtime %.1fs)", r.StaticBuild, r.RuntimeBuild)
			writeStatus("+", r.Name, fmt.Sprintf("schemas match %s", timing), false)
			passed++
		} else {
			writeStatus("x", r.Name, "schemas differ", false)
			failed++
			if r.Diff != "" {
				fmt.Println()
				for line := range strings.SplitSeq(r.Diff, "\n") {
					fmt.Printf("      %s\n", line)
				}
				fmt.Println()
			}
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("-", 40))
	total := passed + failed
	fmt.Printf("%d/%d passed", passed, total)
	if failed > 0 {
		fmt.Printf(", %d FAILED", failed)
	}
	fmt.Println()
	fmt.Println()
}

// SchemaCompareJSONReport generates JSON report for schema comparison
func SchemaCompareJSONReport(results []SchemaCompareResult, cogVersion string) map[string]any {
	models := []map[string]any{}
	for _, r := range results {
		entry := map[string]any{
			"name":            r.Name,
			"passed":          r.Passed,
			"static_build_s":  round(r.StaticBuild, 2),
			"runtime_build_s": round(r.RuntimeBuild, 2),
		}
		if r.Error != "" {
			entry["error"] = r.Error
		}
		if r.Diff != "" {
			entry["diff"] = r.Diff
		}
		models = append(models, entry)
	}

	total := len(results)
	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}

	return map[string]any{
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"cog_version": cogVersion,
		"summary": map[string]int{
			"total":  total,
			"passed": passed,
			"failed": total - passed,
		},
		"models": models,
	}
}

// WriteSchemaCompareJSONReport writes schema comparison JSON report
func WriteSchemaCompareJSONReport(results []SchemaCompareResult, cogVersion string, w io.Writer) error {
	report := SchemaCompareJSONReport(results, cogVersion)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func writeStatus(icon, name, detail string, gpu bool) {
	gpuTag := ""
	if gpu {
		gpuTag = " [GPU]"
	}
	fmt.Printf("  %s %-25s %s%s\n", icon, name, detail, gpuTag)
}

func round(val float64, precision int) float64 {
	p := math.Pow(10, float64(precision))
	return math.Round(val*p) / p
}

// SaveResults saves results to a JSON file in the results directory
func SaveResults(results []ModelResult, sdkVersion, cogVersion string) (string, error) {
	resultsDir := "results"
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating results dir: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s/%s.json", resultsDir, timestamp)

	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("creating results file: %w", err)
	}

	writeErr := WriteJSONReport(results, sdkVersion, cogVersion, f)
	if closeErr := f.Close(); closeErr != nil && writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return "", fmt.Errorf("writing results: %w", writeErr)
	}

	return filename, nil
}
