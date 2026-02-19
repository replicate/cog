package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/util/console"
)

func main() {
	var output string

	rootCmd := &cobra.Command{
		Use:   "gendocs",
		Short: "Generate CLI reference documentation for Cog",
		Run: func(cmd *cobra.Command, args []string) {
			if err := generateDocs(output); err != nil {
				console.Fatalf("Failed to generate docs: %s", err)
			}
			console.Infof("Generated CLI docs at %s", output)
		},
	}

	rootCmd.Flags().StringVarP(&output, "output", "o", "docs/cli.md", "Output file path")
	if err := rootCmd.Execute(); err != nil {
		console.Fatal(err.Error())
	}
}

func generateDocs(outputPath string) error {
	// Create temporary directory for cobra doc generation
	tmpDir, err := os.MkdirTemp("", "cog-cli-docs-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Get the cog command
	cmd, err := cli.NewRootCommand()
	if err != nil {
		return fmt.Errorf("failed to create root command: %w", err)
	}

	// Generate markdown files using cobra/doc
	if err := doc.GenMarkdownTree(cmd, tmpDir); err != nil {
		return fmt.Errorf("failed to generate markdown: %w", err)
	}

	// Read all generated files
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read temp dir: %w", err)
	}

	// Sort files to ensure consistent ordering
	// Order: cog (root), then alphabetically by command name
	var fileNames []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".md") {
			fileNames = append(fileNames, file.Name())
		}
	}
	sort.Strings(fileNames)

	// Build the combined markdown content
	var content strings.Builder

	// Write header
	content.WriteString("# CLI reference\n\n")
	content.WriteString("<!-- This file is auto-generated. Do not edit manually. -->\n\n")

	// Process each command file
	for _, fileName := range fileNames {
		filePath := filepath.Join(tmpDir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", fileName, err)
		}

		// Process the content
		processed := processCommandDoc(string(data), fileName)
		content.WriteString(processed)
		content.WriteString("\n")
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write the combined file
	if err := os.WriteFile(outputPath, []byte(content.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

func processCommandDoc(content string, fileName string) string {
	// Remove the "SEE ALSO" section and everything after it
	if idx := strings.Index(content, "### SEE ALSO"); idx != -1 {
		content = content[:idx]
	}

	// Remove the "Options inherited from parent commands" section
	if idx := strings.Index(content, "### Options inherited from parent commands"); idx != -1 {
		content = content[:idx]
	}

	// Remove trailing whitespace
	content = strings.TrimRight(content, "\n")

	// Fix command headers to use backticks
	// Change "## cog init" to "## `cog init`"
	// Change "### Options" to "**Options**" (not a heading, won't appear in TOC)
	// Change "### Examples" to "**Examples**" (not a heading, won't appear in TOC)
	// Remove "### Synopsis" heading but keep its content
	// Skip the short description if there's a Synopsis section (to avoid duplication)
	lines := strings.Split(content, "\n")
	var result []string
	skipSynopsis := false
	skipShortDesc := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "## cog"):
			// Extract the command name
			command := strings.TrimPrefix(line, "## ")
			result = append(result, "## `"+command+"`")
			// Check if next non-empty line is "### Synopsis" - if so, skip the short desc
			skipShortDesc = hasSynopsisSection(lines)
		case skipShortDesc:
			// Skip the short description line (first non-empty line after header)
			// Also skip any blank lines that follow the header
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "###") {
				// This is the short description line, skip it
				skipShortDesc = false
			}
			// If line is blank, we continue skipping until we hit the short desc
		case line == "### Synopsis":
			// Skip the "### Synopsis" heading line, but keep content after it
			skipSynopsis = true
		case skipSynopsis:
			// Keep synopsis content until we hit the usage block (```) or another heading
			switch {
			case line == "### Examples":
				skipSynopsis = false
				// Add blank line before if needed
				if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
					result = append(result, "")
				}
				result = append(result, "**Examples**")
			case strings.HasPrefix(line, "###"), strings.HasPrefix(line, "```"):
				skipSynopsis = false
				// Add blank line before if needed
				if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
					result = append(result, "")
				}
				result = append(result, line)
			default:
				// Keep all lines from synopsis (including blank lines for paragraph breaks)
				result = append(result, line)
			}
		case line == "### Options":
			// Add blank line before if needed
			if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
				result = append(result, "")
			}
			result = append(result, "**Options**")
		case line == "### Examples":
			// Add blank line before if needed
			if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
				result = append(result, "")
			}
			result = append(result, "**Examples**")
		default:
			result = append(result, line)
		}
	}

	// Remove consecutive blank lines
	result = removeConsecutiveBlankLines(result)

	return strings.Join(result, "\n")
}

// removeConsecutiveBlankLines removes consecutive blank lines, keeping only one
func removeConsecutiveBlankLines(lines []string) []string {
	var result []string
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			// Skip consecutive blank lines
			continue
		}
		result = append(result, line)
		prevBlank = isBlank
	}
	return result
}

// hasSynopsisSection checks if the content has a "### Synopsis" section
func hasSynopsisSection(lines []string) bool {
	return slices.Contains(lines, "### Synopsis")
}
