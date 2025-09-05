package internal

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/anaskhan96/soup"

	"github.com/hashicorp/go-version"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/util/console"
)

var ErrorBadPytorchFormat = errors.New("The pytorch version format could not be parsed.")

func FetchTorchCompatibilityMatrix() ([]config.TorchCompatibility, error) {
	compats := []config.TorchCompatibility{}
	var err error
	compats, err = fetchCurrentTorchVersions(compats)
	if err != nil {
		return nil, err
	}
	compats, err = fetchPreviousTorchVersions(compats)
	if err != nil {
		return nil, err
	}

	// sanity check
	if len(compats) < 21 {
		return nil, fmt.Errorf("PyTorch compatibility matrix only had %d rows, has the html changed?", len(compats))
	}

	return compats, nil
}

func FetchTorchPackages(name string) ([]TorchPackage, error) {
	url := pytorchURL(name)
	return fetchTorchPackagesFromURL(url)
}

func getLatestVersion(packages []TorchPackage) string {
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

	torchPackages, err := FetchTorchPackages("torch")
	if err != nil {
		return nil, fmt.Errorf("Error fetching PyTorch packages: %w", err)
	}
	torchVisionPackages, err := FetchTorchPackages("torchvision")
	if err != nil {
		return nil, fmt.Errorf("Error fetching PyTorch packages: %w", err)
	}
	torchAudioPackages, err := FetchTorchPackages("torchaudio")
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
			if !slices.Contains(val.Pythons, pkg.PythonVersion) {
				val.Pythons = append(val.Pythons, pkg.PythonVersion)
			}
			torchCompats[pkg.Name] = val
		} else {
			torchCompats[pkg.Name] = config.TorchCompatibility{
				Torch:         pkg.Name,
				Torchvision:   latestTorchvisionVersion,
				Torchaudio:    latestTorchaudioVersion,
				CUDA:          pkg.CUDA,
				ExtraIndexURL: pytorchURL(pkg.Variant),
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
		case "--index-url", "--extra-index-url":
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

	pythons, err := FindCompatiblePythonVersions(torch, torchvision, torchaudio, extraIndexURL, findLinks)
	if err != nil {
		return nil, err
	}

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
			switch {
			case strings.HasPrefix(rawArch, "cuda"):
				_, c := split2(rawArch, " ")
				cuda = &c
			case rawArch == "cpu only":
				cuda = nil
			case strings.HasPrefix(rawArch, "rocm"):
				cuda = nil
				skipSection = true
			default:
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
		compat.Torchvision = strings.ReplaceAll(compat.Torchvision, "0.8.0", "0.8.1")
	}
}

func basePytorchURL() string {
	return env.SchemeFromEnvironment() + "://" + env.PytorchHostFromEnvironment() + "/whl"
}

func pytorchURL(name string) string {
	url := fmt.Sprintf(basePytorchURL()+"/%s/", name)
	return url
}

func ExtractSubFeaturesFromPytorchVersion(pytorchVersion string) (string, string, string, string, string, error) {
	decoded, err := url.PathUnescape(pytorchVersion)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("failed to decode filename: %w", err)
	}

	pkgRegexp := regexp.MustCompile(
		`.+?-(?P<basever>\d+(?:\.\d+)*)(?P<suffix>(?:[._]?(?:post|dev|rc)\d+)*)?(?:\+(?P<variant>[a-z0-9_.]+))?-(?P<pyver>[a-z0-9_.]+)-[a-z0-9_.]+-(?P<platform>.+?)\.whl`,
	)

	matches := pkgRegexp.FindStringSubmatch(decoded)
	if len(matches) == 0 {
		return "", "", "", "", "", fmt.Errorf("invalid PyTorch wheel filename: %s", decoded)
	}

	groupMap := make(map[string]string)
	for i, name := range pkgRegexp.SubexpNames() {
		if i != 0 && name != "" {
			groupMap[name] = matches[i]
		}
	}

	base := groupMap["basever"]
	suffix := groupMap["suffix"]
	variant := groupMap["variant"]
	pyverRaw := groupMap["pyver"]
	platform := groupMap["platform"]

	name := base + suffix
	if variant != "" {
		name += "+" + variant
	}
	version := base

	pyver := pyverRaw
	if strings.HasPrefix(pyverRaw, "cp") {
		pyver = pyverRaw[len("cp"):]
	}

	return name, version, variant, pyver, platform, nil
}

func FindCompatiblePythonVersions(torchVersion string, torchVisionVersion string, torchAudioVersion string, extraIndexUrl string, findLinksUrl string) ([]string, error) {
	if extraIndexUrl == "" && findLinksUrl == "" {
		extraIndexUrl = basePytorchURL()
	}
	url := extraIndexUrl
	if url == "" {
		url = findLinksUrl
	}

	// Correct 0.8.0 torchvision to 0.8.1, this is a bug on pytorch.org
	if strings.HasPrefix(torchVisionVersion, "0.8.0") {
		torchVisionVersion = strings.ReplaceAll(torchVisionVersion, "0.8.0", "0.8.1")
	}

	torchPkgs, err := findTorchPackagesWithVersion("torch", url, torchVersion, url != findLinksUrl)
	if err != nil {
		return nil, err
	}

	torchVisionPkgs, err := findTorchPackagesWithVersion("torchvision", url, torchVisionVersion, url != findLinksUrl)
	if err != nil {
		return nil, err
	}

	torchAudioPkgs, err := findTorchPackagesWithVersion("torchaudio", url, torchAudioVersion, url != findLinksUrl)
	if err != nil {
		return nil, err
	}

	// Get initial list of valid python versions from torch
	pythonVersions := map[string]bool{}
	for _, pkg := range torchPkgs {
		pythonVersions[pkg.PythonVersion] = true
	}

	// Check that torchaudio/torchvision shares these python versions
	extraPkgs := [][]TorchPackage{}
	if torchVisionVersion != "" {
		extraPkgs = append(extraPkgs, torchVisionPkgs)
	}
	if torchAudioVersion != "" {
		extraPkgs = append(extraPkgs, torchAudioPkgs)
	}
	for _, pkgs := range extraPkgs {
		pkgPythonVersions := map[string]bool{}
		for _, pkg := range pkgs {
			pkgPythonVersions[pkg.PythonVersion] = true
		}
		for pythonVersion := range pythonVersions {
			_, ok := pkgPythonVersions[pythonVersion]
			if !ok {
				delete(pythonVersions, pythonVersion)
			}
		}
	}

	validPythonVersions := make([]string, 0, len(pythonVersions))
	for k := range pythonVersions {
		validPythonVersions = append(validPythonVersions, k)
	}
	sort.Strings(validPythonVersions)

	return validPythonVersions, nil
}

func fetchTorchPackagesFromURL(url string) ([]TorchPackage, error) {
	resp, err := soup.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to download %s: %w", url, err)
	}
	doc := soup.HTMLParse(resp)
	links := doc.FindAll("a")
	packages := []TorchPackage{}
	for _, link := range links {
		name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion(link.Text())
		if err != nil {
			console.Warnf("Failed to parse pytorch version: %v", err)
			continue
		}
		if (platform != "linux_x86_64" && platform != "manylinux_2_28_x86_64" && platform != "manylinux1_x86_64") || strings.Contains(name, ".cxx") {
			continue
		}

		var cuda *string
		switch {
		case variant == "cpu":
			cuda = nil
		case variant == "":
			cuda = nil
		case strings.HasPrefix(variant, "cu"):
			// cu92 -> 9.2
			c := strings.TrimPrefix(variant, "cu")
			c = c[:len(c)-1] + "." + c[len(c)-1:]
			cuda = &c
		default:
			// rocm etc
			continue
		}

		// 310 -> 3.10
		pythonVersion = pythonVersion[:1] + "." + pythonVersion[1:]

		pkg := TorchPackage{
			Name:          name,
			Version:       version,
			Variant:       variant,
			CUDA:          cuda,
			PythonVersion: pythonVersion,
		}

		found := false
		for _, currentPkg := range packages {
			if currentPkg.Equals(pkg) {
				found = true
				break
			}
		}
		if found {
			continue
		}

		packages = append(packages, pkg)
	}
	return packages, nil
}

func findTorchPackagesWithVersion(pkgName string, url string, version string, appendPkg bool) ([]TorchPackage, error) {
	if appendPkg {
		url = url + "/" + pkgName
	}
	pkgs, err := fetchTorchPackagesFromURL(url)
	if err != nil {
		return nil, err
	}
	validPkgs := []TorchPackage{}
	for _, pkg := range pkgs {
		if pkg.Version != version && pkg.Name != version {
			continue
		}
		validPkgs = append(validPkgs, pkg)
	}
	return validPkgs, nil
}
