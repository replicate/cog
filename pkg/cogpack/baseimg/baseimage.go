package baseimg

// BaseImage contains the selected base images and their metadata
type BaseImage struct {
	Build    string            `json:"build"`    // dev image with build tools
	Runtime  string            `json:"runtime"`  // minimal runtime image
	Metadata BaseImageMetadata `json:"metadata"` // what's pre-installed
}

// BaseImageMetadata describes what packages are available in the base image
type BaseImageMetadata struct {
	Packages map[string]Package `json:"packages"` // "python", "cuda", "git", etc.
}

// Package represents an installed package with metadata
type Package struct {
	Name         string `json:"name"`                    // "python", "cuda"
	Version      string `json:"version"`                 // "3.11.8", "11.8"
	Source       string `json:"source"`                  // "apt", "base-image", "uv"
	Executable   string `json:"executable,omitempty"`    // "/usr/bin/python3"
	SitePackages string `json:"site_packages,omitempty"` // "/usr/local/lib/python3.11/site-packages"
	LibPath      string `json:"lib_path,omitempty"`      // "/usr/local/cuda/lib64"
}

// GetBaseImageMetadata returns metadata for a given base image reference.
// This is a mock implementation that will be replaced by the cogpack-images project.
func GetBaseImageMetadata(imageRef string) (*BaseImageMetadata, error) {
	// Hardcoded metadata for common base images
	// TODO: Replace with actual cogpack-images integration

	switch imageRef {
	case "cogpack/python:3.11-cuda11.8":
		return &BaseImageMetadata{
			Packages: map[string]Package{
				"python": {
					Name:         "python",
					Version:      "3.11.8",
					Source:       "base-image",
					Executable:   "/usr/bin/python3",
					SitePackages: "/usr/local/lib/python3.11/site-packages",
				},
				"cuda": {
					Name:    "cuda",
					Version: "11.8",
					Source:  "base-image",
					LibPath: "/usr/local/cuda/lib64",
				},
				"build-essential": {
					Name:    "build-essential",
					Version: "12.9ubuntu3",
					Source:  "apt",
				},
				"git": {
					Name:    "git",
					Version: "2.34.1",
					Source:  "apt",
				},
				"uv": {
					Name:       "uv",
					Version:    "0.4.0",
					Source:     "base-image",
					Executable: "/usr/local/bin/uv",
				},
			},
		}, nil

	case "cogpack/python:3.11":
		return &BaseImageMetadata{
			Packages: map[string]Package{
				"python": {
					Name:         "python",
					Version:      "3.11.8",
					Source:       "base-image",
					Executable:   "/usr/bin/python3",
					SitePackages: "/usr/local/lib/python3.11/site-packages",
				},
				"build-essential": {
					Name:    "build-essential",
					Version: "12.9ubuntu3",
					Source:  "apt",
				},
				"git": {
					Name:    "git",
					Version: "2.34.1",
					Source:  "apt",
				},
				"uv": {
					Name:       "uv",
					Version:    "0.4.0",
					Source:     "base-image",
					Executable: "/usr/local/bin/uv",
				},
			},
		}, nil

	case "cogpack/python:3.12":
		return &BaseImageMetadata{
			Packages: map[string]Package{
				"python": {
					Name:         "python",
					Version:      "3.12.1",
					Source:       "base-image",
					Executable:   "/usr/bin/python3",
					SitePackages: "/usr/local/lib/python3.12/site-packages",
				},
				"build-essential": {
					Name:    "build-essential",
					Version: "12.9ubuntu3",
					Source:  "apt",
				},
				"git": {
					Name:    "git",
					Version: "2.34.1",
					Source:  "apt",
				},
				"uv": {
					Name:       "uv",
					Version:    "0.4.0",
					Source:     "base-image",
					Executable: "/usr/local/bin/uv",
				},
			},
		}, nil

	case "ubuntu:22.04":
		return &BaseImageMetadata{
			Packages: map[string]Package{
				"build-essential": {
					Name:    "build-essential",
					Version: "12.9ubuntu3",
					Source:  "apt",
				},
			},
		}, nil

	default:
		// Fallback for unknown images - minimal Ubuntu
		return &BaseImageMetadata{
			Packages: map[string]Package{
				"build-essential": {
					Name:    "build-essential",
					Version: "12.9ubuntu3",
					Source:  "apt",
				},
			},
		}, nil
	}
}

// SelectBaseImage chooses the best base image based on resolved dependencies.
// For initial implementation, we use known working r8.im base images.
func SelectBaseImage(dependencies map[string]string) (BaseImage, error) {
	// Extract Python version if available
	pythonVersion := ""
	if python, exists := dependencies["python"]; exists {
		pythonVersion = python
	}

	// Use known working base images from r8.im registry
	var buildImage, runtimeImage string

	if pythonVersion != "" {
		// Use Python base images - these are known to exist and work
		buildImage = "r8.im/cog-base:python3.13.4-ubuntu22.04-dev"
		runtimeImage = "r8.im/cog-base:python3.13.4-ubuntu22.04-run"
	} else {
		// Fallback to basic Ubuntu images
		buildImage = "r8.im/cog-base:ubuntu22.04-dev"
		runtimeImage = "r8.im/cog-base:ubuntu22.04-run"
	}

	// Create basic metadata - for now just mark that Python is available
	metadata := BaseImageMetadata{
		Packages: map[string]Package{
			"python": {
				Name:         "python",
				Version:      "3.13.4",
				Source:       "base-image",
				Executable:   "/usr/bin/python3",
				SitePackages: "/usr/local/lib/python3.13/site-packages",
			},
		},
	}

	return BaseImage{
		Build:    buildImage,
		Runtime:  runtimeImage,
		Metadata: metadata,
	}, nil
}
