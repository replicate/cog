package kong

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

//go:embed init-templates/**/*
var initTemplates embed.FS

// InitCmd implements `cog init`.
type InitCmd struct{}

func (c *InitCmd) Run(g *Globals) error {
	console.Infof("\nSetting up the current directory for use with Cog...\n")

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	templateDir := path.Join("init-templates", "base")
	entries, err := initTemplates.ReadDir(templateDir)
	if err != nil {
		return fmt.Errorf("Error reading template directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			if err := processTemplateDir(initTemplates, templateDir, entry.Name(), cwd); err != nil {
				return err
			}
			continue
		}
		if err := processTemplateFile(initTemplates, templateDir, entry.Name(), cwd); err != nil {
			return err
		}
	}

	console.Infof("\nDone! For next steps, check out the docs at https://cog.run/getting-started")
	return nil
}

func processTemplateDir(fs embed.FS, templateDir, subDir, cwd string) error {
	subDirPath := path.Join(templateDir, subDir)
	entries, err := fs.ReadDir(subDirPath)
	if err != nil {
		return fmt.Errorf("Error reading subdirectory %s: %w", subDirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			if err := processTemplateDir(fs, subDirPath, entry.Name(), cwd); err != nil {
				return err
			}
			continue
		}
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
	switch {
	case filename == "AGENTS.md":
		downloadedContent, err := downloadAgentsFile()
		if err != nil {
			console.Infof("Failed to download AGENTS.md: %v", err)
			console.Infof("Using template version instead...")
			content, err = fs.ReadFile(path.Join(templateDir, filename))
			if err != nil {
				return fmt.Errorf("Error reading template %s: %w", filename, err)
			}
		} else {
			content = downloadedContent
		}
	default:
		content, err = fs.ReadFile(path.Join(templateDir, filename))
		if err != nil {
			return fmt.Errorf("Error reading %s: %w", filename, err)
		}
	}

	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		return fmt.Errorf("Error writing %s: %w", filePath, err)
	}
	console.Infof("Created %s", filePath)
	return nil
}

func downloadAgentsFile() ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://replicate.com/docs/reference/cog/llms.txt")
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
