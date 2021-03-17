package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/anaskhan96/soup"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/server"
)

func main() {
	tfOutputPath := flag.String("tf-output", "", "Tensorflow output path")
	torchOutputPath := flag.String("torch-output", "", "PyTorch output path")
	cudaImagesOutputPath := flag.String("cuda-images-output", "", "CUDA base images output path")
	flag.Parse()

	if *tfOutputPath == "" && *torchOutputPath == "" && *cudaImagesOutputPath == "" {
		log.Fatal("at least one of -tf-output, -torch-output, -cuda-images-output must be provided")
	}

	if *tfOutputPath != "" {
		if err := writeTFCompatibilityMatrix(*tfOutputPath); err != nil {
			log.Fatalf("Failed to write Tensorflow compatibility matrix: %s", err)
		}
	}
	if *torchOutputPath != "" {
		if err := writeTorchCompatibilityMatrix(*torchOutputPath); err != nil {
			log.Fatalf("Failed to write PyTorch compatibility matrix: %s", err)
		}
	}
	if *cudaImagesOutputPath != "" {
		if err := writeCUDABaseImageTags(*cudaImagesOutputPath); err != nil {
			log.Fatalf("Failed to write CUDA base images: %s", err)
		}
	}
}

func writeTFCompatibilityMatrix(outputPath string) error {
	log.Infof("Writing Tensorflow compatibility matrix to %s...", outputPath)

	url := "https://www.tensorflow.org/install/source"
	resp, err := soup.Get(url)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %w", url, err)
	}
	doc := soup.HTMLParse(resp)
	gpuHeading := doc.Find("h4", "id", "gpu")
	table := gpuHeading.FindNextElementSibling()
	rows := table.FindAll("tr")

	compats := []server.TFCompatibility{}
	for _, row := range rows[1:] {
		cells := row.FindAll("td")
		gpuPackage, packageVersion := split2(cells[0].Text(), "-")
		pythonVersions, err := parsePythonVersionsCell(cells[1].Text())
		if err != nil {
			return err
		}
		cuDNN := cells[4].Text()
		cuda := cells[5].Text()

		compat := server.TFCompatibility{
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
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return err
	}
	return nil
}

func writeTorchCompatibilityMatrix(outputPath string) error {
	log.Infof("Writing PyTorch compatibility matrix to %s...", outputPath)

	compats := []server.TorchCompatibility{}
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
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return err
	}
	return nil
}

func writeCUDABaseImageTags(outputPath string) error {
	log.Infof("Writing CUDA base images to %s...", outputPath)
	url := "https://hub.docker.com/v2/repositories/nvidia/cuda/tags/?page_size=1000&name=devel-ubuntu&ordering=last_updated"
	resp, err := soup.Get(url)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %w", url, err)
	}
	var results struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		return fmt.Errorf("Failed parse CUDA images json: %w", err)
	}

	tags := []string{}
	for _, result := range results.Results {
		tag := result.Name
		if strings.Contains(tag, "-cudnn") && !strings.HasSuffix(tag, "-rc") {
			tags = append(tags, tag)
		}
	}

	data, err := json.MarshalIndent(tags, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return err
	}

	return nil
}

func fetchCurrentTorchVersions(compats []server.TorchCompatibility) ([]server.TorchCompatibility, error) {
	url := "https://pytorch.org/assets/quick-start-module.js"

	resp, err := soup.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to download %s: %w", url, err)
	}
	objRe := regexp.MustCompile(`(?s)function commandMessage\(key\) {
  var object = ({.*});`)
	objRaw := objRe.FindStringSubmatch(resp)[1]
	objRaw = strings.Replace(objRaw, `,
  }`, "}", 1) // remove final trailing comma
	obj := map[string]string{}
	if err := json.Unmarshal([]byte(objRaw), &obj); err != nil {
		return nil, err
	}

	defaultVersions := map[string]string{}
	for _, lib := range []string{"torch", "torchvision", "torchaudio"} {
		latestVersionRe := regexp.MustCompile(lib + `==([0-9]+\.[0-9]+\.[0-9]+)`)
		latestVersions := latestVersionRe.FindAllStringSubmatch(resp, -1)
		latestVersion := latestVersions[0][1]
		for _, v := range latestVersions[1:] {
			if latestVersion != v[1] {
				return nil, fmt.Errorf("%s versions aren't all the same, has the JS changed?", lib)
			}
		}
		defaultVersions[lib] = latestVersion
	}

	for key, val := range obj {
		if strings.HasPrefix(key, "stable,pip,linux") && strings.HasSuffix(key, ",python") {
			parts := strings.Split(key, ",")
			cudaRaw := parts[3]
			var cuda *string
			if strings.HasPrefix(cudaRaw, "cuda") {
				c := cudaRaw[4:]
				cuda = &c // can't take pointer directly
			} else if cudaRaw != "accnone" {
				continue // rocm, etc.
			}
			compat, err := parseTorchInstallString(val, defaultVersions, cuda)
			if err != nil {
				return nil, err
			}
			compats = append(compats, *compat)
		}
	}
	return compats, nil
}

func parseTorchInstallString(s string, defaultVersions map[string]string, cuda *string) (*server.TorchCompatibility, error) {
	// e.g. "pip install torch==1.8.0+cpu torchvision==0.9.0+cpu torchaudio==0.8.0 -f https://download.pytorch.org/whl/torch_stable.html"

	libVersions := map[string]string{}

	s = strings.TrimSpace(s)
	s = strings.Split(s, "pip install ")[1]
	parts := strings.Split(s, " -f ")
	libs := strings.Split(parts[0], " ")
	for _, lib := range libs {
		libParts := strings.Split(lib, "==")
		libName := libParts[0]
		if _, ok := defaultVersions[libName]; !ok {
			return nil, fmt.Errorf("Unknown library: %s", libName)
		}
		if len(libParts) == 1 {
			libVersions[libName] = defaultVersions[libName]
		} else {
			libVersions[libName] = libParts[1]
		}
	}

	indexURL := ""
	if len(parts) > 1 {
		indexURL = parts[1]
	}

	torch, ok := libVersions["torch"]
	if !ok {
		return nil, fmt.Errorf("Missing torch version")
	}
	torchvision, ok := libVersions["torchvision"]
	if !ok {
		return nil, fmt.Errorf("Missing torchvision version")
	}
	torchaudio, _ := libVersions["torchaudio"]

	// TODO(andreas): maybe scrape this from https://pytorch.org/get-started/locally/
	pythons := []string{"3.6", "3.7", "3.8", "3.9"}

	return &server.TorchCompatibility{
		Torch:       torch,
		Torchvision: torchvision,
		Torchaudio:  torchaudio,
		IndexURL:    indexURL,
		CUDA:        cuda,
		Pythons:     pythons,
	}, nil
}

func fetchPreviousTorchVersions(compats []server.TorchCompatibility) ([]server.TorchCompatibility, error) {
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

func parsePreviousTorchVersionsCode(code string, compats []server.TorchCompatibility) ([]server.TorchCompatibility, error) {
	// e.g.
	// # CUDA 10.1
	// pip install torch==1.5.0+cu101 torchvision==0.6.0+cu101 -f https://download.pytorch.org/whl/torch_stable.html

	supportedLibrarySet := map[string]string{
		"torch": "", "torchvision": "", "torchaudio": "",
	}

	sections := strings.Split(code, "\n\n")
	for _, section := range sections {
		heading, install := split2(section, "\n")
		if !strings.HasPrefix(install, "pip install ") {
			// conda install etc
			continue
		}
		rawArch := heading[2:]
		var cuda *string
		if strings.HasPrefix(rawArch, "CUDA") {
			_, c := split2(rawArch, " ")
			cuda = &c // can't take pointer directly
		} else if rawArch != "CPU only" {
			return nil, fmt.Errorf("Invalid arch: %s", rawArch)
		}
		compat, err := parseTorchInstallString(install, supportedLibrarySet, cuda)

		fixTorchCompatibility(compat)

		if err != nil {
			return nil, err
		}
		compats = append(compats, *compat)
	}
	return compats, nil
}

// torchvision==0.8.0 should actually be 0.8.1, this is a bug on the website
func fixTorchCompatibility(compat *server.TorchCompatibility) {
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
