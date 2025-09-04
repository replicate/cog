package cli

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

var (
	//go:embed init-templates/**/*
	initTemplates    embed.FS
	pipelineTemplate bool
)

func newInitCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:        "init",
		SuggestFor: []string{"new", "start"},
		Short:      "Configure your project for use with Cog",
		RunE:       initCommand,
		Args:       cobra.MaximumNArgs(0),
	}

	addPipelineInit(cmd)
	return cmd
}

func initCommand(cmd *cobra.Command, args []string) error {
	console.Infof("\nSetting up the current directory for use with Cog...\n")

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	initTemplate := "base"
	if pipelineTemplate {
		initTemplate = "pipeline"
	}

	// Discover all files in the embedded template directory
	templateDir := path.Join("init-templates", initTemplate)
	entries, err := initTemplates.ReadDir(templateDir)
	if err != nil {
		return fmt.Errorf("Error reading template directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Recursively process subdirectories
			if err := processTemplateDirectory(initTemplates, templateDir, entry.Name(), cwd); err != nil {
				return err
			}
			continue
		}

		// Process individual files
		if err := processTemplateFile(initTemplates, templateDir, entry.Name(), cwd); err != nil {
			return err
		}
	}

	console.Infof("\nDone! For next steps, check out the docs at https://cog.run/getting-started")

	return nil
}

func processTemplateDirectory(fs embed.FS, templateDir, subDir, cwd string) error {
	subDirPath := path.Join(templateDir, subDir)
	entries, err := fs.ReadDir(subDirPath)
	if err != nil {
		return fmt.Errorf("Error reading subdirectory %s: %w", subDirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Recursively process nested subdirectories
			if err := processTemplateDirectory(fs, subDirPath, entry.Name(), cwd); err != nil {
				return err
			}
			continue
		}

		// Process files in subdirectories
		relativePath := path.Join(subDir, entry.Name())
		if err := processTemplateFile(fs, templateDir, relativePath, cwd); err != nil {
			return err
		}
	}

	return nil
}

func processTemplateFile(fs embed.FS, templateDir, filename, cwd string) error {
	filePath := path.Join(cwd, filename)
	fileExists, err := files.Exists(filePath)
	if err != nil {
		return fmt.Errorf("Error checking if %s exists: %w", filePath, err)
	}

	if fileExists {
		console.Infof("Skipped existing %s", filename)
		return nil
	}

	dirPath := path.Dir(filePath)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		return fmt.Errorf("Error creating directory %s: %w", dirPath, err)
	}

	var content []byte

	// Special handling for specific template files
	switch {
	case filename == "AGENTS.md":
		// Try to download from Replicate docs
		downloadedContent, err := downloadAgentsFile()
		if err != nil {
			console.Infof("Failed to download AGENTS.md: %v", err)
			console.Infof("Using template version instead...")
			// Fall back to template version
			content, err = fs.ReadFile(path.Join(templateDir, filename))
			if err != nil {
				return fmt.Errorf("Error reading template %s: %w", filename, err)
			}
		} else {
			content = downloadedContent
		}
	case filename == "requirements.txt" && pipelineTemplate:
		// Special handling for requirements.txt in pipeline templates - download from runtime
		downloadedContent, err := downloadPipelineRequirementsFile()
		if err != nil {
			console.Infof("Failed to download pipeline requirements.txt: %v", err)
			console.Infof("Using template version instead...")
			// Fall back to template version
			content, err = fs.ReadFile(path.Join(templateDir, filename))
			if err != nil {
				return fmt.Errorf("Error reading template %s: %w", filename, err)
			}
		} else {
			content = downloadedContent
		}
	default:
		// Regular template file processing
		content, err = fs.ReadFile(path.Join(templateDir, filename))
		if err != nil {
			return fmt.Errorf("Error reading %s: %w", filename, err)
		}
	}

	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		return fmt.Errorf("Error writing %s: %w", filePath, err)
	}

	console.Infof("✅ Created %s", filePath)
	return nil
}

func downloadAgentsFile() ([]byte, error) {
	const agentsURL = "https://replicate.com/docs/reference/pipelines/llms.txt"

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(agentsURL)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return content, nil
}

func downloadPipelineRequirementsFile() ([]byte, error) {
	requirementsURL := pipelinesRuntimeRequirementsURL()

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(requirementsURL.String())
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return content, nil
}

func pipelinesRuntimeRequirementsURL() url.URL {
	baseURL := url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   env.PipelinesRuntimeHostFromEnvironment(),
	}
	baseURL.Path = "requirements.txt"
	return baseURL
}

func addPipelineInit(cmd *cobra.Command) {
	const pipeline = "x-pipeline"
	cmd.Flags().BoolVar(&pipelineTemplate, pipeline, false, "Initialize a pipeline template")
	_ = cmd.Flags().MarkHidden(pipeline)
}
