package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	githubAPI  = "https://api.github.com/repos/replicate/cog/releases"
	githubRepo = "https://github.com/replicate/cog.git"
	pypiAPI    = "https://pypi.org/pypi/cog/json"
)

var prereleaseRe = regexp.MustCompile(`(?i)-(alpha|beta|rc|dev)`)

// checksumMismatchError indicates the downloaded file's checksum does not match the expected value.
// This is a hard error (possible corruption or tampering), distinct from the checksum file being
// unavailable or the asset name not being found.
type checksumMismatchError struct {
	Asset    string
	Expected string
	Actual   string
}

func (e *checksumMismatchError) Error() string {
	return fmt.Sprintf("checksum mismatch for %s: expected %s, got %s", e.Asset, e.Expected, e.Actual)
}

// Result holds resolved versions and paths
type Result struct {
	CogBinary  string
	CogVersion string
	SDKVersion string
	// SDKPatchVersion is the version to inject into cog.yaml build.sdk_version.
	// This is intentionally empty when a wheel is used without explicit --sdk-version.
	SDKPatchVersion string
	SDKWheel        string
}

// Resolve resolves the cog binary and SDK version to use
func Resolve(cogBinary, cogVersion, cogRef, sdkVersion, sdkWheel string, manifestDefaults map[string]string) (*Result, error) {
	result := &Result{}

	// Resolve cog binary
	binary, version, wheel, err := resolveCogBinary(cogBinary, cogVersion, cogRef, manifestDefaults)
	if err != nil {
		return nil, fmt.Errorf("resolving cog binary: %w", err)
	}
	result.CogBinary = binary
	result.CogVersion = version

	// Determine SDK wheel: explicit --sdk-wheel wins over ref-built
	if sdkWheel != "" {
		result.SDKWheel = sdkWheel
		result.SDKVersion = fmt.Sprintf("wheel:%s", filepath.Base(sdkWheel))
		if sdkVersion != "" {
			result.SDKPatchVersion = sdkVersion
			result.SDKVersion = sdkVersion
		}
	} else if wheel != "" {
		result.SDKWheel = wheel
		result.SDKVersion = version
		if sdkVersion != "" {
			result.SDKPatchVersion = sdkVersion
			result.SDKVersion = sdkVersion
		}
	} else {
		// Resolve SDK version from PyPI or explicit flag
		sdkVer, err := resolveSDKVersion(sdkVersion, manifestDefaults)
		if err != nil {
			return nil, fmt.Errorf("resolving SDK version: %w", err)
		}
		result.SDKVersion = sdkVer
		result.SDKPatchVersion = sdkVer
	}

	return result, nil
}

func resolveCogBinary(cogBinary, cogVersion, cogRef string, manifestDefaults map[string]string) (string, string, string, error) {
	// 1. Explicit --cog-binary (non-default)
	if cogBinary != "" && cogBinary != "cog" {
		return cogBinary, "custom", "", nil
	}

	// 2. Explicit --cog-ref (build from source)
	if cogRef != "" {
		return buildCogFromRef(cogRef)
	}

	// 3. Explicit --cog-version
	if cogVersion != "" {
		tag := cogVersion
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		path, err := downloadCogBinary(tag)
		return path, tag, "", err
	}

	// 4. Manifest default
	if manifestDefaults != nil {
		if manifestVersion := manifestDefaults["cog_version"]; manifestVersion != "" && manifestVersion != "latest" {
			tag := manifestVersion
			if !strings.HasPrefix(tag, "v") {
				tag = "v" + tag
			}
			path, err := downloadCogBinary(tag)
			return path, tag, "", err
		}
	}

	// 5. Resolve latest stable
	tag, err := resolveLatestCogVersion()
	if err != nil {
		return "", "", "", err
	}
	path, err := downloadCogBinary(tag)
	return path, tag, "", err
}

func resolveSDKVersion(sdkVersion string, manifestDefaults map[string]string) (string, error) {
	// 1. Explicit --sdk-version
	if sdkVersion != "" {
		return sdkVersion, nil
	}

	// 2. Manifest default
	if manifestDefaults != nil {
		if manifestVersion := manifestDefaults["sdk_version"]; manifestVersion != "" && manifestVersion != "latest" {
			return manifestVersion, nil
		}
	}

	// 3. Resolve latest stable from PyPI
	return resolveLatestPyPIVersion()
}

func setGitHubAuth(req *http.Request) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := os.Getenv("GH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func resolveLatestCogVersion() (string, error) {
	url := fmt.Sprintf("%s?per_page=50", githubAPI)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	setGitHubAuth(req)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decoding GitHub releases: %w", err)
	}

	for _, release := range releases {
		tag, _ := release["tag_name"].(string)
		prerelease, _ := release["prerelease"].(bool)
		draft, _ := release["draft"].(bool)

		if prerelease || draft {
			continue
		}
		if prereleaseRe.MatchString(tag) {
			continue
		}
		return tag, nil
	}

	return "", fmt.Errorf("no stable release found")
}

func resolveLatestPyPIVersion() (string, error) {
	req, err := http.NewRequest("GET", pypiAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching PyPI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PyPI API returned %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding PyPI response: %w", err)
	}

	info, ok := data["info"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid PyPI response format")
	}

	version, ok := info["version"].(string)
	if !ok {
		return "", fmt.Errorf("version not found in PyPI response")
	}

	return version, nil
}

func downloadCogBinary(tag string) (dest string, err error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Normalize OS names to match release asset naming (e.g. Darwin, Linux)
	osMap := map[string]string{
		"darwin": "Darwin",
		"linux":  "Linux",
	}
	if normalized, ok := osMap[osName]; ok {
		osName = normalized
	}

	// Normalize architecture names
	archMap := map[string]string{
		"amd64": "x86_64",
		"arm64": "arm64",
	}
	if normalized, ok := archMap[arch]; ok {
		arch = normalized
	}

	assetName := fmt.Sprintf("cog_%s_%s", osName, arch)
	url := fmt.Sprintf("https://github.com/replicate/cog/releases/download/%s/%s", tag, assetName)

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	baseDir := filepath.Join(home, ".cache", "cog-harness", "bin")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("creating bin cache dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(baseDir, "cog-bin-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmpDir)
		}
	}()

	dest = filepath.Join(tmpDir, "cog")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	setGitHubAuth(req)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading cog binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return "", fmt.Errorf("creating binary file: %w", err)
	}

	if _, copyErr := io.Copy(f, resp.Body); copyErr != nil {
		f.Close()
		return "", fmt.Errorf("writing binary: %w", copyErr)
	}

	if closeErr := f.Close(); closeErr != nil {
		return "", fmt.Errorf("closing binary file: %w", closeErr)
	}

	if err := verifyDownloadedBinary(tag, assetName, dest); err != nil {
		var mismatchErr *checksumMismatchError
		if errors.As(err, &mismatchErr) {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "WARNING: could not verify checksum: %v\n", err)
		fmt.Fprintf(os.Stderr, "WARNING: continuing without checksum verification\n")
	} else {
		fmt.Fprintf(os.Stderr, "Checksum verified for %s\n", assetName)
	}

	return dest, nil
}

func verifyDownloadedBinary(tag, assetName, dest string) error {
	checksumURL := fmt.Sprintf("https://github.com/replicate/cog/releases/download/%s/checksums.txt", tag)
	req, err := http.NewRequest("GET", checksumURL, nil)
	if err != nil {
		return fmt.Errorf("building checksum request: %w", err)
	}
	setGitHubAuth(req)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading checksum file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum download returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading checksum file: %w", err)
	}

	expected, err := parseChecksum(string(body), assetName)
	if err != nil {
		return err
	}

	actual, err := fileSHA256(dest)
	if err != nil {
		return err
	}

	if !strings.EqualFold(expected, actual) {
		return &checksumMismatchError{Asset: assetName, Expected: expected, Actual: actual}
	}

	return nil
}

func parseChecksum(content, assetName string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimPrefix(parts[1], "*")
		if name == assetName {
			hash := parts[0]
			if len(hash) != 64 {
				return "", fmt.Errorf("invalid checksum length for %s: got %d chars, expected 64", assetName, len(hash))
			}
			if _, err := hex.DecodeString(hash); err != nil {
				return "", fmt.Errorf("invalid checksum hex for %s: %w", assetName, err)
			}
			return hash, nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found in checksums.txt", assetName)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening downloaded binary: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing downloaded binary: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func buildCogFromRef(ref string) (string, string, string, error) {
	tmpDir, err := os.MkdirTemp("", "cog-ref-*")
	if err != nil {
		return "", "", "", fmt.Errorf("creating temp dir: %w", err)
	}

	// Check prerequisites
	for _, tool := range []string{"go", "uv"} {
		if _, err := exec.LookPath(tool); err != nil {
			return "", "", "", fmt.Errorf("'%s' not found on PATH", tool)
		}
	}

	cloneDir := filepath.Join(tmpDir, "cog-src")

	// Try shallow clone first
	cloneCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", "clone", "--single-branch", "--depth=1", "--branch", ref, githubRepo, cloneDir)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Try full clone + checkout
		fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer fallbackCancel()
		cmd = exec.CommandContext(fallbackCtx, "git", "clone", githubRepo, cloneDir)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", "", "", fmt.Errorf("cloning repo: %w", err)
		}

		cmd = exec.CommandContext(fallbackCtx, "git", "checkout", ref)
		cmd.Dir = cloneDir
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", "", "", fmt.Errorf("checking out ref: %w", err)
		}
	}

	// Build CLI binary
	cogBinary := filepath.Join(tmpDir, "cog")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer buildCancel()
	cmd = exec.CommandContext(buildCtx, "go", "build", "-o", cogBinary, "./cmd/cog")
	cmd.Dir = cloneDir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", "", fmt.Errorf("building cog CLI: %w", err)
	}

	// Read VERSION.txt and convert to PEP 440
	version := "0.0.0.dev0"
	versionFile := filepath.Join(cloneDir, "VERSION.txt")
	if data, err := os.ReadFile(versionFile); err == nil {
		version = strings.TrimSpace(string(data))
		version = strings.ReplaceAll(version, "-alpha", "a")
		version = strings.ReplaceAll(version, "-beta", "b")
		version = strings.ReplaceAll(version, "-rc", "rc")
		version = strings.ReplaceAll(version, "-dev", ".dev")
	}

	// Build SDK wheel
	wheelDir := filepath.Join(tmpDir, "dist")
	if err := os.MkdirAll(wheelDir, 0755); err != nil {
		return "", "", "", fmt.Errorf("creating wheel dir: %w", err)
	}

	uvCtx, uvCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer uvCancel()
	cmd = exec.CommandContext(uvCtx, "uv", "build", "--out-dir", wheelDir, ".")
	cmd.Dir = cloneDir
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "SETUPTOOLS_SCM_PRETEND_VERSION="+version)
	if err := cmd.Run(); err != nil {
		return "", "", "", fmt.Errorf("building SDK wheel: %w", err)
	}

	// Find the wheel
	entries, err := os.ReadDir(wheelDir)
	if err != nil {
		return "", "", "", fmt.Errorf("reading wheel dir: %w", err)
	}

	var wheelPath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".whl") && strings.HasPrefix(entry.Name(), "cog-") {
			wheelPath = filepath.Join(wheelDir, entry.Name())
			break
		}
	}

	if wheelPath == "" {
		return "", "", "", fmt.Errorf("no wheel found in %s", wheelDir)
	}

	return cogBinary, "ref:" + ref, wheelPath, nil
}
