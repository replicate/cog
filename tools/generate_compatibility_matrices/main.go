package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/anaskhan96/soup"

	"github.com/hashicorp/go-version"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/console"
)

func main() {
	tfOutputPath := flag.String("tf-output", "pkg/config/tf_compatability_matrix.json", "Tensorflow output path")
	torchOutputPath := flag.String("torch-output", "pkg/config/torch_compatability_matrix.json", "PyTorch output path")
	cudaImagesOutputPath := flag.String("cuda-images-output", "pkg/config/cuda_base_image_tags.json", "CUDA base images output path")
	flag.Parse()

	if *tfOutputPath == "" && *torchOutputPath == "" && *cudaImagesOutputPath == "" {
		console.Fatal("at least one of -tf-output, -torch-output, -cuda-images-output must be provided")
	}

	if *tfOutputPath != "" {
		if err := writeTFCompatibilityMatrix(*tfOutputPath); err != nil {
			console.Fatalf("Failed to write Tensorflow compatibility matrix: %s", err)
		}
	}
	if *torchOutputPath != "" {
		if err := writeTorchCompatibilityMatrix(*torchOutputPath); err != nil {
			console.Fatalf("Failed to write PyTorch compatibility matrix: %s", err)
		}
	}
	if *cudaImagesOutputPath != "" {
		if err := writeCUDABaseImageTags(*cudaImagesOutputPath); err != nil {
			console.Fatalf("Failed to write CUDA base images: %s", err)
		}
	}
}

func writeTFCompatibilityMatrix(outputPath string) error {
	console.Infof("Writing Tensorflow compatibility matrix to %s...", outputPath)

	url := "https://www.tensorflow.org/install/source"
	resp, err := soup.Get(url)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %w", url, err)
	}
	doc := soup.HTMLParse(resp)
	gpuHeading := doc.Find("h4", "id", "gpu")
	table := gpuHeading.FindNextElementSibling()
	rows := table.FindAll("tr")

	compats := []config.TFCompatibility{}
	for _, row := range rows[1:] {
		cells := row.FindAll("td")
		gpuPackage, packageVersion := split2(cells[0].Text(), "-")
		pythonVersions, err := parsePythonVersionsCell(cells[1].Text())
		if err != nil {
			return err
		}
		cuDNN := cells[4].Text()
		cuda := cells[5].Text()

		compat := config.TFCompatibility{
			TF:           packageVersion,
			TFCPUPackage: "tensorflow==" + packageVersion,
			TFGPUPackage: gpuPackage + "==" + packageVersion,
			CUDA:         cuda,
			CuDNN:        cuDNN,
			Pythons:      pythonVersions,
		}
		compats = append(compats, compat)
	}

	// sanity check
	if len(compats) < 21 {
		return fmt.Errorf("Tensorflow compatibility matrix only had %d rows, has the html changed?", len(compats))
	}

	data, err := json.MarshalIndent(compats, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return err
	}
	return nil
}

func writeTorchCompatibilityMatrix(outputPath string) error {
	console.Infof("Writing PyTorch compatibility matrix to %s...", outputPath)

	compats := []config.TorchCompatibility{}
	var err error
	compats, err = fetchCurrentTorchVersions(compats)
	if err != nil {
		return err
	}
	compats, err = fetchPreviousTorchVersions(compats)
	if err != nil {
		return err
	}

	// sanity check
	if len(compats) < 21 {
		return fmt.Errorf("PyTorch compatibility matrix only had %d rows, has the html changed?", len(compats))
	}

	data, err := json.MarshalIndent(compats, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return err
	}
	return nil
}

func getCUDABaseImageTags(url string) ([]string, error) {
	tags := []string{}

	resp, err := soup.Get(url)
	if err != nil {
		return tags, fmt.Errorf("Failed to download %s: %w", url, err)
	}

	var results struct {
		Next    *string
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		return tags, fmt.Errorf("Failed parse CUDA images json: %w", err)
	}

	for _, result := range results.Results {
		tag := result.Name
		if strings.Contains(tag, "-cudnn") && !strings.HasSuffix(tag, "-rc") {
			tags = append(tags, tag)
		}
	}

	// recursive case for pagination
	if results.Next != nil {
		nextURL := *results.Next
		nextTags, err := getCUDABaseImageTags(nextURL)
		if err != nil {
			return tags, err
		}
		tags = append(tags, nextTags...)
	}

	return tags, nil
}

func writeCUDABaseImageTags(outputPath string) error {
	console.Infof("Writing CUDA base images to %s...", outputPath)
	url := "https://hub.docker.com/v2/repositories/nvidia/cuda/tags/?page_size=1000&name=devel-ubuntu&ordering=last_updated"

	tags, err := getCUDABaseImageTags(url)
	if err != nil {
		return err
	}

	sort.Sort(sort.Reverse(sort.StringSlice(tags)))

	data, err := json.MarshalIndent(tags, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return err
	}

	return nil
}

type torchPackage struct {
	Name          string
	Version       string
	Variant       string
	CUDA          *string
	PythonVersion string
}

func fetchTorchPackages(name string) ([]torchPackage, error) {
	pkgRegexp := regexp.MustCompile(`(.+?)-(([0-9.]+)\+([a-z0-9]+))-cp([0-9.]+)-cp([0-9.]+)-linux_x86_64.whl`)

	url := fmt.Sprintf("https://download.pytorch.org/whl/%s/", name)
	resp, err := soup.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to download %s: %w", url, err)
	}
	doc := soup.HTMLParse(resp)
	links := doc.FindAll("a")
	packages := []torchPackage{}
	for _, link := range links {
		groups := pkgRegexp.FindStringSubmatch(link.Text())
		if len(groups) == 0 {
			continue
		}
		name, version, variant, pythonVersion := groups[2], groups[3], groups[4], groups[5]

		var cuda *string
		if variant == "cpu" {
			cuda = nil
		} else if strings.HasPrefix(variant, "cu") {
			// cu92 -> 9.2
			c := strings.TrimPrefix(variant, "cu")
			c = c[:len(c)-1] + "." + c[len(c)-1:]
			cuda = &c
		} else {
			// rocm etc
			continue
		}

		// 310 -> 3.10
		pythonVersion = pythonVersion[:1] + "." + pythonVersion[1:]

		packages = append(packages, torchPackage{
			Name:          name,
			Version:       version,
			Variant:       variant,
			CUDA:          cuda,
			PythonVersion: pythonVersion,
		})
	}
	return packages, nil
}

func getLatestVersion(packages []torchPackage) string {
	latestVersion, _ := version.NewVersion("0.0.0")
	for _, pkg := range packages {
		v, err := version.NewVersion(pkg.Version)
		if err != nil {
			fmt.Println("error parsing version:", pkg.Version)
			continue
		}
		if v.GreaterThan(latestVersion) {
			latestVersion = v
		}
	}
	return latestVersion.String()
}

func fetchCurrentTorchVersions(compats []config.TorchCompatibility) ([]config.TorchCompatibility, error) {
	// For the latest PyTorch version, we can just grab the latest of each packages from the repository.
	// We then install the packages in the same way as we do for 1.12.x:
	// https://pytorch.org/get-started/previous-versions/#v1121

	torchPackages, err := fetchTorchPackages("torch")
	if err != nil {
		return nil, fmt.Errorf("Error fetching PyTorch packages: %w", err)
	}
	torchVisionPackages, err := fetchTorchPackages("torchvision")
	if err != nil {
		return nil, fmt.Errorf("Error fetching PyTorch packages: %w", err)
	}
	torchAudioPackages, err := fetchTorchPackages("torchaudio")
	if err != nil {
		return nil, fmt.Errorf("Error fetching PyTorch packages: %w", err)
	}

	latestTorchVersion := getLatestVersion(torchPackages)
	latestTorchvisionVersion := getLatestVersion(torchVisionPackages)
	latestTorchaudioVersion := getLatestVersion(torchAudioPackages)

	torchCompats := map[string]config.TorchCompatibility{}

	for _, pkg := range torchPackages {
		if pkg.Version != latestTorchVersion {
			continue
		}

		if val, ok := torchCompats[pkg.Name]; ok {
			val.Pythons = append(val.Pythons, pkg.PythonVersion)
			torchCompats[pkg.Name] = val
		} else {
			torchCompats[pkg.Name] = config.TorchCompatibility{
				Torch:         pkg.Name,
				Torchvision:   latestTorchvisionVersion,
				Torchaudio:    latestTorchaudioVersion,
				CUDA:          pkg.CUDA,
				ExtraIndexURL: "https://download.pytorch.org/whl/" + pkg.Variant,
				Pythons:       []string{pkg.PythonVersion},
			}

		}
	}

	for _, compat := range torchCompats {
		compats = append(compats, compat)
	}

	return compats, nil
}

func parseTorchInstallString(s string, defaultVersions map[string]string, cuda *string) (*config.TorchCompatibility, error) {
	// for example:
	// pip3 install torch torchvision torchaudio --extra-index-url https://download.pytorch.org/whl/cu113
	// pip install torch==1.8.0+cpu torchvision==0.9.0+cpu torchaudio==0.8.0 -f https://download.pytorch.org/whl/torch_stable.html

	libVersions := map[string]string{}

	findLinks := ""
	extraIndexURL := ""
	skipNext := false

	// Simple parser for pip install strings
	fields := strings.Fields(s)
	for i, item := range fields {
		// Ideally we want to be able to consume the next token, but golang has no simple way of doing that without constructing a channel
		if skipNext {
			skipNext = false
			continue
		}
		switch item {
		case "pip", "pip3", "install":
			continue
		case "-f":
			findLinks = fields[i+1]
			skipNext = true
			continue
		case "--extra-index-url":
			extraIndexURL = fields[i+1]
			skipNext = true
			continue
		}

		libParts := strings.Split(item, "==")
		libName := libParts[0]
		if _, ok := defaultVersions[libName]; !ok {
			return nil, fmt.Errorf("Unknown token when parsing torch string: %s", item)
		}
		if len(libParts) == 1 {
			libVersions[libName] = defaultVersions[libName]
		} else {
			libVersions[libName] = libParts[1]
		}

	}

	torch, ok := libVersions["torch"]
	if !ok {
		return nil, fmt.Errorf("Missing torch version")
	}
	torchvision, ok := libVersions["torchvision"]
	if !ok {
		return nil, fmt.Errorf("Missing torchvision version")
	}
	torchaudio := libVersions["torchaudio"]

	// TODO: this could be determined from https://download.pytorch.org/whl/torch/
	pythons := []string{"3.6", "3.7", "3.8", "3.9", "3.10"}

	return &config.TorchCompatibility{
		Torch:         torch,
		Torchvision:   torchvision,
		Torchaudio:    torchaudio,
		FindLinks:     findLinks,
		ExtraIndexURL: extraIndexURL,
		CUDA:          cuda,
		Pythons:       pythons,
	}, nil
}

func fetchPreviousTorchVersions(compats []config.TorchCompatibility) ([]config.TorchCompatibility, error) {
	// For previous versions, we need to scrape the PyTorch website.
	// The reason we can't fetch it from the PyPI repository like the latest version is
	// because we don't know what versions of torch, torchvision, and torchaudio are compatible with each other.

	url := "https://pytorch.org/get-started/previous-versions/"
	resp, err := soup.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to download %s: %w", url, err)
	}
	doc := soup.HTMLParse(resp)

	for _, h5 := range doc.FindAll("h5") {
		if strings.TrimSpace(h5.Text()) == "Linux and Windows" {
			highlight := h5.FindNextElementSibling()
			code := highlight.Find("code")
			compats, err = parsePreviousTorchVersionsCode(code.Text(), compats)
			if err != nil {
				return nil, err
			}
		}
	}
	return compats, nil
}

func parsePreviousTorchVersionsCode(code string, compats []config.TorchCompatibility) ([]config.TorchCompatibility, error) {
	// e.g.
	// # CUDA 10.1
	// pip install torch==1.5.0+cu101 torchvision==0.6.0+cu101 -f https://download.pytorch.org/whl/torch_stable.html

	supportedLibrarySet := map[string]string{
		"torch": "", "torchvision": "", "torchaudio": "",
	}

	var cuda *string
	skipSection := false

	for _, line := range strings.Split(code, "\n") {
		// Set section
		if strings.HasPrefix(line, "#") {
			skipSection = false
			rawArch := strings.ToLower(line[2:])
			if strings.HasPrefix(rawArch, "cuda") {
				_, c := split2(rawArch, " ")
				cuda = &c
			} else if rawArch == "cpu only" {
				cuda = nil
			} else if strings.HasPrefix(rawArch, "rocm") {
				cuda = nil
				skipSection = true
			} else {
				// Ignore additional heading lines (notes, etc)
				continue
			}
		}

		// In a ROCM section, so skip
		if skipSection {
			continue
		}

		// conda install etc
		if !strings.HasPrefix(line, "pip install ") {
			continue
		}
		compat, err := parseTorchInstallString(line, supportedLibrarySet, cuda)
		if err != nil {
			return nil, err
		}
		fixTorchCompatibility(compat)

		compats = append(compats, *compat)
	}
	return compats, nil
}

// torchvision==0.8.0 should actually be 0.8.1, this is a bug on the website
func fixTorchCompatibility(compat *config.TorchCompatibility) {
	if strings.HasPrefix(compat.Torchvision, "0.8.0") {
		compat.Torchvision = strings.Replace(compat.Torchvision, "0.8.0", "0.8.1", -1)
	}
}

func parsePythonVersionsCell(val string) ([]string, error) {
	versions := []string{}
	parts := strings.Split(val, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			start, end := split2(part, "-")
			startMajor, startMinor, err := splitPythonVersion(start)
			if err != nil {
				return nil, err
			}
			endMajor, endMinor, err := splitPythonVersion(end)
			if err != nil {
				return nil, err
			}

			if startMajor != endMajor {
				return nil, fmt.Errorf("Invalid start and end minor versions: %d, %d", startMajor, endMajor)
			}
			for minor := startMinor; minor <= endMinor; minor++ {
				versions = append(versions, newVersion(startMajor, minor))
			}
		} else {
			versions = append(versions, part)
		}
	}
	return versions, nil
}

func newVersion(major int, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
}

func splitPythonVersion(version string) (major int, minor int, err error) {
	version = strings.TrimSpace(version)
	majorStr, minorStr := split2(version, ".")
	major, err = strconv.Atoi(majorStr)
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(minorStr)
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}

func split2(s string, sep string) (string, string) {
	parts := strings.SplitN(s, sep, 2)
	return parts[0], parts[1]
}
