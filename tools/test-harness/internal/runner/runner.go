package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
	"github.com/replicate/cog/tools/test-harness/internal/patcher"
	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/validator"
)

const openapiSchemaLabel = "run.cog.openapi_schema"

// Runner orchestrates the test lifecycle
type Runner struct {
	cogBinary   string
	sdkVersion  string
	sdkWheel    string
	fixturesDir string
	workDir     string
	keepImages  bool
	clonedRepos map[string]string
}

// New creates a new Runner
func New(cogBinary, sdkVersion, sdkWheel string, fixturesDir string, keepImages bool) (*Runner, error) {
	if fixturesDir == "" {
		var err error
		fixturesDir, err = resolveFixturesDir()
		if err != nil {
			return nil, err
		}
	}

	// Create the work directory under $HOME so that Docker volume mounts work
	// with VM-based runtimes like Colima that only share $HOME by default.
	// Using the system temp dir ($TMPDIR, e.g. /var/folders/... on macOS) would
	// result in empty volume mounts when cog predict/train/serve runs containers
	// with source directories mounted as /src.
	baseDir := filepath.Join(os.Getenv("HOME"), ".cache", "cog-harness")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating harness cache dir: %w", err)
	}
	workDir, err := os.MkdirTemp(baseDir, "run-*")
	if err != nil {
		return nil, fmt.Errorf("creating work dir: %w", err)
	}

	return &Runner{
		cogBinary:   cogBinary,
		sdkVersion:  sdkVersion,
		sdkWheel:    sdkWheel,
		fixturesDir: fixturesDir,
		workDir:     workDir,
		keepImages:  keepImages,
		clonedRepos: make(map[string]string),
	}, nil
}

func resolveFixturesDir() (string, error) {
	if cwd, err := os.Getwd(); err == nil {
		candidates := []string{
			filepath.Join(cwd, "fixtures"),
			filepath.Join(cwd, "tools", "test-harness", "fixtures"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				absPath, err := filepath.Abs(candidate)
				if err == nil {
					return absPath, nil
				}
			}
		}
	}

	_, filename, _, ok := runtime.Caller(0)
	if ok {
		sourceDir := filepath.Dir(filename)
		candidate := filepath.Join(sourceDir, "..", "..", "fixtures")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			absPath, err := filepath.Abs(candidate)
			if err == nil {
				return absPath, nil
			}
		}
	}

	return "", fmt.Errorf("fixtures directory not found: specify fixtures dir or run from project root/tool dir")
}

// Cleanup removes temp directories and Docker images
func (r *Runner) Cleanup() error {
	if !r.keepImages {
		// Remove Docker images
		cmd := exec.Command("docker", "images", "--filter", "reference=cog-harness-*", "--format", "{{.Repository}}:{{.Tag}}")
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			images := strings.Fields(string(output))
			if len(images) > 0 {
				args := append([]string{"rmi", "--force"}, images...)
				exec.Command("docker", args...).Run()
			}
		}
	}

	// Remove work directory
	if r.workDir != "" {
		os.RemoveAll(r.workDir)
	}

	return nil
}

// RunModel runs all tests for a single model
func (r *Runner) RunModel(ctx context.Context, model manifest.Model) *report.ModelResult {
	result := &report.ModelResult{
		Name:   model.Name,
		Passed: true,
		GPU:    model.GPU,
	}

	// Check required env vars
	for _, envVar := range model.RequiresEnv {
		if os.Getenv(envVar) == "" {
			result.Passed = true
			result.Skipped = true
			result.SkipReason = fmt.Sprintf("Missing env var: %s", envVar)
			return result
		}
	}

	// Prepare model
	modelDir, err := r.prepareModel(model)
	if err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Preparation failed: %v", err)
		return result
	}

	// Build
	buildStart := time.Now()
	if err := r.buildModel(ctx, modelDir, model); err != nil {
		result.Passed = false
		result.BuildDuration = time.Since(buildStart).Seconds()
		result.Error = fmt.Sprintf("Build failed: %v", err)
		return result
	}
	result.BuildDuration = time.Since(buildStart).Seconds()

	// Run train tests
	for _, tc := range model.TrainTests {
		tr := r.runTrainTest(ctx, modelDir, model, tc)
		result.TrainResults = append(result.TrainResults, tr)
		if !tr.Passed {
			result.Passed = false
		}
	}

	// Run predict tests
	for _, tc := range model.Tests {
		tr := r.runPredictTest(ctx, modelDir, model, tc)
		result.TestResults = append(result.TestResults, tr)
		if !tr.Passed {
			result.Passed = false
		}
	}

	return result
}

// BuildModel builds a model image only
func (r *Runner) BuildModel(ctx context.Context, model manifest.Model) *report.ModelResult {
	result := &report.ModelResult{
		Name:   model.Name,
		Passed: true,
		GPU:    model.GPU,
	}

	// Check required env vars
	for _, envVar := range model.RequiresEnv {
		if os.Getenv(envVar) == "" {
			result.Passed = true
			result.Skipped = true
			result.SkipReason = fmt.Sprintf("Missing env var: %s", envVar)
			return result
		}
	}

	// Prepare model
	modelDir, err := r.prepareModel(model)
	if err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Preparation failed: %v", err)
		return result
	}

	// Build
	buildStart := time.Now()
	if err := r.buildModel(ctx, modelDir, model); err != nil {
		result.Passed = false
		result.BuildDuration = time.Since(buildStart).Seconds()
		result.Error = fmt.Sprintf("Build failed: %v", err)
		return result
	}
	result.BuildDuration = time.Since(buildStart).Seconds()

	return result
}

// CompareSchema builds model twice and compares schemas
func (r *Runner) CompareSchema(ctx context.Context, model manifest.Model) *report.SchemaCompareResult {
	result := &report.SchemaCompareResult{
		Name:   model.Name,
		Passed: true,
	}

	// Prepare model
	modelDir, err := r.prepareModel(model)
	if err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Preparation failed: %v", err)
		return result
	}

	staticTag := fmt.Sprintf("cog-harness-%s:static", model.Name)
	runtimeTag := fmt.Sprintf("cog-harness-%s:runtime", model.Name)
	staticDir := filepath.Join(r.workDir, fmt.Sprintf("schema-static-%s", model.Name))
	runtimeDir := filepath.Join(r.workDir, fmt.Sprintf("schema-runtime-%s", model.Name))
	if err := copyDir(modelDir, staticDir); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("preparing static schema dir failed: %v", err)
		return result
	}
	if err := copyDir(modelDir, runtimeDir); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("preparing runtime schema dir failed: %v", err)
		return result
	}

	// Build both variants concurrently
	g, ctx := errgroup.WithContext(ctx)

	var staticSchema, runtimeSchema string
	var staticSchemaErr, runtimeSchemaErr error
	var staticErr, runtimeErr error
	staticStart := time.Now()
	runtimeStart := time.Now()

	g.Go(func() error {
		staticErr = r.buildModelWithEnv(ctx, staticDir, model, staticTag, map[string]string{"COG_STATIC_SCHEMA": "1"})
		if staticErr != nil {
			return nil // Don't fail the group, we'll check errors after
		}
		staticSchema, staticSchemaErr = r.extractSchemaLabel(staticTag)
		result.StaticBuild = time.Since(staticStart).Seconds()
		return nil
	})

	g.Go(func() error {
		runtimeErr = r.buildModelWithEnv(ctx, runtimeDir, model, runtimeTag, map[string]string{})
		if runtimeErr != nil {
			return nil
		}
		runtimeSchema, runtimeSchemaErr = r.extractSchemaLabel(runtimeTag)
		result.RuntimeBuild = time.Since(runtimeStart).Seconds()
		return nil
	})

	if err := g.Wait(); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("context cancelled: %v", err)
		return result
	}

	// Clean up images
	defer func() {
		exec.Command("docker", "rmi", "-f", staticTag).Run()
		exec.Command("docker", "rmi", "-f", runtimeTag).Run()
	}()

	// Check for build errors
	if staticErr != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Static build failed: %v", staticErr)
		return result
	}
	if runtimeErr != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Runtime build failed: %v", runtimeErr)
		return result
	}
	if staticSchemaErr != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("extracting static schema label failed: %v", staticSchemaErr)
		return result
	}
	if runtimeSchemaErr != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("extracting runtime schema label failed: %v", runtimeSchemaErr)
		return result
	}

	// Parse and compare schemas
	var staticJSON, runtimeJSON map[string]any
	if err := json.Unmarshal([]byte(staticSchema), &staticJSON); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Static schema is not valid JSON: %v", err)
		return result
	}
	if err := json.Unmarshal([]byte(runtimeSchema), &runtimeJSON); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Runtime schema is not valid JSON: %v", err)
		return result
	}

	// Compare
	diff := jsonDiff(staticJSON, runtimeJSON)
	if diff != "" {
		result.Passed = false
		result.Diff = diff
	}

	return result
}

func (r *Runner) prepareModel(model manifest.Model) (string, error) {
	// Local fixture models
	if model.Repo == "local" {
		fixturesModels := filepath.Join(r.fixturesDir, "models")
		modelDir, err := safeSubpath(fixturesModels, model.Path)
		if err != nil {
			return "", err
		}

		if _, err := os.Stat(filepath.Join(modelDir, "cog.yaml")); err != nil {
			return "", fmt.Errorf("no cog.yaml in %s", modelDir)
		}

		// Copy to work dir
		dest := filepath.Join(r.workDir, fmt.Sprintf("local-%s", model.Name))
		if err := copyDir(modelDir, dest); err != nil {
			return "", fmt.Errorf("copying model: %w", err)
		}

		// Patch cog.yaml
		sdkVersion := model.SDKVersion
		if sdkVersion == "" {
			sdkVersion = r.sdkVersion
		}
		if err := patcher.Patch(filepath.Join(dest, "cog.yaml"), sdkVersion, model.CogYAMLOverrides); err != nil {
			return "", fmt.Errorf("patching cog.yaml: %w", err)
		}

		return dest, nil
	}

	// Clone repo
	repoDir, err := r.cloneRepo(model.Repo)
	if err != nil {
		return "", err
	}

	modelDir := filepath.Join(repoDir, model.Path)
	if _, err := os.Stat(filepath.Join(modelDir, "cog.yaml")); err != nil {
		return "", fmt.Errorf("no cog.yaml in %s", modelDir)
	}

	// Patch cog.yaml
	sdkVersion := model.SDKVersion
	if sdkVersion == "" {
		sdkVersion = r.sdkVersion
	}
	if err := patcher.Patch(filepath.Join(modelDir, "cog.yaml"), sdkVersion, model.CogYAMLOverrides); err != nil {
		return "", fmt.Errorf("patching cog.yaml: %w", err)
	}

	return modelDir, nil
}

func (r *Runner) cloneRepo(repo string) (string, error) {
	if dir, ok := r.clonedRepos[repo]; ok {
		return dir, nil
	}

	dest := filepath.Join(r.workDir, strings.ReplaceAll(repo, "/", "--"))

	// Remove if exists
	os.RemoveAll(dest)

	url := fmt.Sprintf("https://github.com/%s.git", repo)
	cmd := exec.Command("git", "clone", "--depth=1", url, dest)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cloning %s: %w", repo, err)
	}

	r.clonedRepos[repo] = dest
	return dest, nil
}

func (r *Runner) buildModel(ctx context.Context, modelDir string, model manifest.Model) error {
	imageTag := fmt.Sprintf("cog-harness-%s:test", model.Name)
	return r.buildModelWithEnv(ctx, modelDir, model, imageTag, nil)
}

func (r *Runner) buildModelWithEnv(ctx context.Context, modelDir string, model manifest.Model, imageTag string, extraEnv map[string]string) error {
	// Set timeout
	timeout := model.Timeout
	if timeout == 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.cogBinary, "build", "-t", imageTag)
	cmd.Dir = modelDir
	cmd.Env = os.Environ()
	if r.sdkWheel != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("COG_SDK_WHEEL=%s", r.sdkWheel))
	}
	for k, v := range model.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
	}
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, string(output))
	}
	return nil
}

func (r *Runner) extractSchemaLabel(imageTag string) (string, error) {
	cmd := exec.Command("docker", "inspect", imageTag, "--format", fmt.Sprintf("{{index .Config.Labels \"%s\"}}", openapiSchemaLabel))
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r *Runner) runPredictTest(ctx context.Context, modelDir string, model manifest.Model, tc manifest.TestCase) report.TestResult {
	return r.runCogTest(ctx, modelDir, model, tc, "predict")
}

func (r *Runner) runTrainTest(ctx context.Context, modelDir string, model manifest.Model, tc manifest.TestCase) report.TestResult {
	return r.runCogTest(ctx, modelDir, model, tc, "train")
}

func (r *Runner) runCogTest(ctx context.Context, modelDir string, model manifest.Model, tc manifest.TestCase, command string) report.TestResult {
	result := report.TestResult{
		Description: tc.Description,
	}
	if result.Description == "" {
		result.Description = command
	}

	start := time.Now()

	// Build command
	args := []string{command}
	for key, value := range tc.Inputs {
		resolved := r.resolveInput(value)
		args = append(args, "-i", fmt.Sprintf("%s=%s", key, resolved))
	}

	// Set timeout
	timeout := model.Timeout
	if timeout == 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.cogBinary, args...)
	cmd.Dir = modelDir
	cmd.Env = os.Environ()
	if r.sdkWheel != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("COG_SDK_WHEEL=%s", r.sdkWheel))
	}
	for k, v := range model.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result.DurationSec = time.Since(start).Seconds()

	if ctx.Err() == context.DeadlineExceeded {
		result.Passed = false
		result.Message = fmt.Sprintf("Timed out after %ds", timeout)
		return result
	}

	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("cog %s exited with error: %v\n%s", command, err, stderr.String())
		return result
	}

	// Extract output from stdout only — stderr contains build logs and status
	// messages that should not be matched against expected values.
	outputStr := extractOutput(stdout.String(), stderr.String(), modelDir)

	// Validate
	vr := validator.Validate(outputStr, tc.Expect)
	result.Passed = vr.Passed
	result.Message = vr.Message

	return result
}

func safeSubpath(root, sub string) (string, error) {
	if filepath.IsAbs(sub) {
		return "", fmt.Errorf("local model path must be relative: %q", sub)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving root path: %w", err)
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, sub))
	if err != nil {
		return "", fmt.Errorf("resolving target path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("resolving relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("local model path escapes fixtures root: %q", sub)
	}
	return targetAbs, nil
}

func (r *Runner) resolveInput(value any) string {
	s := fmt.Sprint(value)
	if !strings.HasPrefix(s, "@") {
		return s
	}

	fixtureName := s[1:]
	fixturePath := filepath.Join(r.fixturesDir, fixtureName)
	absPath, err := filepath.Abs(fixturePath)
	if err != nil {
		return s
	}
	return fmt.Sprintf("@%s", absPath)
}

func extractOutput(stdout, stderr, modelDir string) string {
	// For file outputs (e.g. images), cog writes the file to CWD and prints
	// "Written output to: <path>" on stderr. Check stderr for this pattern.
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, "Written output to:") {
			parts := strings.SplitN(line, "Written output to:", 2)
			if len(parts) == 2 {
				path := strings.TrimSpace(parts[1])
				// Make absolute if relative
				if !filepath.IsAbs(path) {
					path = filepath.Join(modelDir, path)
				}
				return path
			}
		}
	}

	// Return stdout (the actual prediction output)
	return strings.TrimSpace(stdout)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func jsonDiff(a, b map[string]any) string {
	var lines []string
	diffRecursive(a, b, "$", &lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func diffRecursive(a, b any, path string, lines *[]string) {
	if a == nil && b == nil {
		return
	}
	if a == nil || b == nil {
		*lines = append(*lines, fmt.Sprintf("  %s: one side is nil", path))
		return
	}

	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			*lines = append(*lines, fmt.Sprintf("  %s: type mismatch (object vs %T)", path, b))
			return
		}
		allKeys := make(map[string]bool)
		for k := range av {
			allKeys[k] = true
		}
		for k := range bv {
			allKeys[k] = true
		}
		for k := range allKeys {
			childPath := fmt.Sprintf("%s.%s", path, k)
			if _, ok := av[k]; !ok {
				*lines = append(*lines, fmt.Sprintf("  %s: missing in static", childPath))
			} else if _, ok := bv[k]; !ok {
				*lines = append(*lines, fmt.Sprintf("  %s: missing in runtime", childPath))
			} else {
				diffRecursive(av[k], bv[k], childPath, lines)
			}
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			*lines = append(*lines, fmt.Sprintf("  %s: type mismatch (array vs %T)", path, b))
			return
		}
		if len(av) != len(bv) {
			*lines = append(*lines, fmt.Sprintf("  %s: array length mismatch (%d vs %d)", path, len(av), len(bv)))
			return
		}
		for i := range av {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			diffRecursive(av[i], bv[i], childPath, lines)
		}
	default:
		if a != b {
			*lines = append(*lines, fmt.Sprintf("  %s: value mismatch (%v vs %v)", path, a, b))
		}
	}
}
