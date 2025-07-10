package cogpack

import "fmt"

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
// This is a mock implementation that will be replaced by the cogpack-images project.
func SelectBaseImage(dependencies map[string]Dependency) (BaseImage, error) {
	// Extract key requirements
	pythonVersion := ""
	cudaVersion := ""

	if python, exists := dependencies["python"]; exists {
		pythonVersion = python.ResolvedVersion
	}

	if cuda, exists := dependencies["cuda"]; exists {
		cudaVersion = cuda.ResolvedVersion
	}

	// Select appropriate images based on requirements
	var buildImage, runtimeImage string

	if pythonVersion != "" && cudaVersion != "" {
		// Python + CUDA
		buildImage = fmt.Sprintf("cogpack/python:%s-cuda%s", pythonVersion, cudaVersion)
		runtimeImage = fmt.Sprintf("cogpack/python:%s-cuda%s-runtime", pythonVersion, cudaVersion)
	} else if pythonVersion != "" {
		// Python only
		buildImage = fmt.Sprintf("cogpack/python:%s", pythonVersion)
		runtimeImage = fmt.Sprintf("cogpack/python:%s-runtime", pythonVersion)
	} else {
		// Fallback to Ubuntu
		buildImage = "ubuntu:22.04"
		runtimeImage = "ubuntu:22.04"
	}

	// Get metadata for the build image
	metadata, err := GetBaseImageMetadata(buildImage)
	if err != nil {
		return BaseImage{}, fmt.Errorf("failed to get metadata for %s: %w", buildImage, err)
	}

	return BaseImage{
		Build:    buildImage,
		Runtime:  runtimeImage,
		Metadata: *metadata,
	}, nil
}
