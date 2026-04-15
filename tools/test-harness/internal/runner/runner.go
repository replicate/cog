package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
	"github.com/replicate/cog/tools/test-harness/internal/patcher"
	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/validator"
)

const openapiSchemaLabel = "run.cog.openapi_schema"

// prefixWriter wraps an io.Writer and prepends a prefix to each line.
// Partial lines (no trailing newline) are buffered until a newline arrives.
type prefixWriter struct {
	prefix string
	dest   io.Writer
	mu     sync.Mutex
	buf    []byte
}

func newPrefixWriter(dest io.Writer, modelName string) *prefixWriter {
	return &prefixWriter{
		prefix: fmt.Sprintf("[%-20s] ", modelName),
		dest:   dest,
	}
}

func (pw *prefixWriter) Write(p []byte) (int, error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	total := len(p)
	pw.buf = append(pw.buf, p...)

	for {
		idx := bytes.IndexByte(pw.buf, '\n')
		if idx < 0 {
			break
		}
		line := pw.buf[:idx]
		pw.buf = pw.buf[idx+1:]
		if _, err := fmt.Fprintf(pw.dest, "%s%s\n", pw.prefix, line); err != nil {
			return total, err
		}
	}
	return total, nil
}

// Flush writes any remaining buffered content (partial line without trailing newline).
func (pw *prefixWriter) Flush() {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if len(pw.buf) > 0 {
		_, _ = fmt.Fprintf(pw.dest, "%s%s\n", pw.prefix, pw.buf)
		pw.buf = nil
	}
}

// modelOutput returns stdout/stderr writers for a model.
// In parallel mode, output is prefixed with the model name and
// also captured in a buffer for error reporting.
// In sequential mode, output streams directly to the terminal
// and is also captured.
func (r *Runner) modelOutput(modelName string) (stdout, stderr io.Writer, capture *bytes.Buffer, flush func()) {
	var buf bytes.Buffer
	if r.opts.Parallel {
		pw := newPrefixWriter(os.Stderr, modelName)
		w := io.MultiWriter(pw, &buf)
		return w, w, &buf, pw.Flush
	}
	return io.MultiWriter(os.Stdout, &buf), io.MultiWriter(os.Stderr, &buf), &buf, func() {}
}

// Options configures a Runner.
type Options struct {
	CogBinary   string
	SDKVersion  string
	SDKWheel    string
	FixturesDir string
	CleanImages bool
	KeepOutputs bool
	Parallel    bool // Prefix output lines with model name (for parallel execution)
}

// Runner orchestrates the test lifecycle.
// It is safe to call RunModel, BuildModel, and CompareSchema concurrently
// from multiple goroutines.
type Runner struct {
	opts        Options
	fixturesDir string
	workDir     string
	clonedRepos map[string]string
	mu          sync.Mutex // protects clonedRepos
}

// New creates a new Runner
func New(opts Options) (*Runner, error) {
	fixturesDir := opts.FixturesDir
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
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory for work dir (set $HOME): %w", err)
	}
	baseDir := filepath.Join(home, ".cache", "cog-harness")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating harness cache dir: %w", err)
	}
	workDir, err := os.MkdirTemp(baseDir, "run-*")
	if err != nil {
		return nil, fmt.Errorf("creating work dir: %w", err)
	}

	return &Runner{
		opts:        opts,
		fixturesDir: fixturesDir,
		workDir:     workDir,
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
	var errs []error
	if r.opts.CleanImages {
		cmd := exec.Command("docker", "images", "--filter", "reference=cog-harness-*", "--format", "{{.Repository}}:{{.Tag}}")
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			images := strings.Fields(string(output))
			if len(images) > 0 {
				args := append([]string{"rmi", "--force"}, images...)
				if err := exec.Command("docker", args...).Run(); err != nil {
					errs = append(errs, fmt.Errorf("removing docker images: %w", err))
				}
			}
		}
	}

	if r.workDir != "" {
		if r.opts.KeepOutputs {
			fmt.Printf("Outputs preserved in: %s\n", r.workDir)
		} else {
			if err := os.RemoveAll(r.workDir); err != nil {
				errs = append(errs, fmt.Errorf("removing work dir: %w", err))
			}
		}
	}

	return errors.Join(errs...)
}

// RunModel runs all tests for a single model
func (r *Runner) RunModel(ctx context.Context, model manifest.Model) *report.ModelResult {
	_, logw, _, flush := r.modelOutput(model.Name)

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
	_, _ = fmt.Fprintf(logw, "=== Preparing %s...\n", model.Name)
	modelDir, err := r.prepareModel(ctx, model)
	if err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Preparation failed: %v", err)
		flush()
		return result
	}

	// Build
	_, _ = fmt.Fprintf(logw, "=== Building %s (timeout %ds)...\n", model.Name, model.Timeout)
	buildStart := time.Now()
	if err := r.buildModel(ctx, modelDir, model); err != nil {
		result.Passed = false
		result.BuildDuration = time.Since(buildStart).Seconds()
		result.Error = fmt.Sprintf("Build failed: %v", err)
		_, _ = fmt.Fprintf(logw, "=== Build FAILED after %.1fs\n", result.BuildDuration)
		flush()
		return result
	}
	result.BuildDuration = time.Since(buildStart).Seconds()
	_, _ = fmt.Fprintf(logw, "=== Build complete (%.1fs)\n", result.BuildDuration)

	// Run train tests
	for i, tc := range model.TrainTests {
		desc := tc.Description
		if desc == "" {
			desc = "train"
		}
		_, _ = fmt.Fprintf(logw, "=== Train test %d/%d: %s (timeout %ds)...\n", i+1, len(model.TrainTests), desc, model.Timeout)
		tr := r.runTrainTest(ctx, modelDir, model, tc)
		result.TrainResults = append(result.TrainResults, tr)
		if tr.Passed {
			_, _ = fmt.Fprintf(logw, "=== Train test %d/%d PASSED (%.1fs)\n", i+1, len(model.TrainTests), tr.DurationSec)
		} else {
			_, _ = fmt.Fprintf(logw, "=== Train test %d/%d FAILED (%.1fs)\n", i+1, len(model.TrainTests), tr.DurationSec)
			result.Passed = false
		}
	}

	// Run predict tests
	for i, tc := range model.Tests {
		desc := tc.Description
		if desc == "" {
			desc = "predict"
		}
		_, _ = fmt.Fprintf(logw, "=== Predict test %d/%d: %s (timeout %ds)...\n", i+1, len(model.Tests), desc, model.Timeout)
		tr := r.runPredictTest(ctx, modelDir, model, tc)
		result.TestResults = append(result.TestResults, tr)
		if tr.Passed {
			_, _ = fmt.Fprintf(logw, "=== Predict test %d/%d PASSED (%.1fs)\n", i+1, len(model.Tests), tr.DurationSec)
		} else {
			_, _ = fmt.Fprintf(logw, "=== Predict test %d/%d FAILED (%.1fs)\n", i+1, len(model.Tests), tr.DurationSec)
			result.Passed = false
		}
	}

	flush()
	return result
}

// BuildModel builds a model image only
func (r *Runner) BuildModel(ctx context.Context, model manifest.Model) *report.ModelResult {
	_, logw, _, flush := r.modelOutput(model.Name)

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
	_, _ = fmt.Fprintf(logw, "=== Preparing %s...\n", model.Name)
	modelDir, err := r.prepareModel(ctx, model)
	if err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Preparation failed: %v", err)
		flush()
		return result
	}

	// Build
	_, _ = fmt.Fprintf(logw, "=== Building %s (timeout %ds)...\n", model.Name, model.Timeout)
	buildStart := time.Now()
	if err := r.buildModel(ctx, modelDir, model); err != nil {
		result.Passed = false
		result.BuildDuration = time.Since(buildStart).Seconds()
		result.Error = fmt.Sprintf("Build failed: %v", err)
		_, _ = fmt.Fprintf(logw, "=== Build FAILED after %.1fs\n", result.BuildDuration)
		flush()
		return result
	}
	result.BuildDuration = time.Since(buildStart).Seconds()
	_, _ = fmt.Fprintf(logw, "=== Build complete (%.1fs)\n", result.BuildDuration)

	flush()
	return result
}

// CompareSchema builds model twice and compares schemas
func (r *Runner) CompareSchema(ctx context.Context, model manifest.Model) *report.SchemaCompareResult {
	result := &report.SchemaCompareResult{
		Name:   model.Name,
		Passed: true,
	}

	// Prepare model
	modelDir, err := r.prepareModel(ctx, model)
	if err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("Preparation failed: %v", err)
		return result
	}

	staticTag := fmt.Sprintf("cog-harness-%s:static", model.Name)
	runtimeTag := fmt.Sprintf("cog-harness-%s:runtime", model.Name)

	// Always clean up schema comparison images when done
	defer func() {
		_ = exec.Command("docker", "rmi", "-f", staticTag).Run()
		_ = exec.Command("docker", "rmi", "-f", runtimeTag).Run()
	}()

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
	var staticDuration, runtimeDuration float64
	staticStart := time.Now()
	runtimeStart := time.Now()

	g.Go(func() error {
		staticErr = r.buildModelWithEnv(ctx, staticDir, model, staticTag, map[string]string{"COG_STATIC_SCHEMA": "1"})
		if staticErr != nil {
			return nil // Don't fail the group, we'll check errors after
		}
		staticSchema, staticSchemaErr = r.extractSchemaLabel(ctx, staticTag)
		staticDuration = time.Since(staticStart).Seconds()
		return nil
	})

	g.Go(func() error {
		runtimeErr = r.buildModelWithEnv(ctx, runtimeDir, model, runtimeTag, map[string]string{})
		if runtimeErr != nil {
			return nil
		}
		runtimeSchema, runtimeSchemaErr = r.extractSchemaLabel(ctx, runtimeTag)
		runtimeDuration = time.Since(runtimeStart).Seconds()
		return nil
	})

	if err := g.Wait(); err != nil {
		result.Passed = false
		result.Error = fmt.Sprintf("context canceled: %v", err)
		return result
	}

	result.StaticBuild = staticDuration
	result.RuntimeBuild = runtimeDuration

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

func (r *Runner) prepareModel(ctx context.Context, model manifest.Model) (string, error) {
	var modelDir string

	// Local fixture models
	if model.Repo == "local" {
		fixturesModels := filepath.Join(r.fixturesDir, "models")
		srcDir, err := safeSubpath(fixturesModels, model.Path)
		if err != nil {
			return "", err
		}

		// Copy to work dir
		dest := filepath.Join(r.workDir, fmt.Sprintf("local-%s", model.Name))
		if err := copyDir(srcDir, dest); err != nil {
			return "", fmt.Errorf("copying model: %w", err)
		}
		modelDir = dest
	} else {
		// Clone repo (shared cache, thread-safe)
		repoDir, err := r.cloneRepo(ctx, model.Repo)
		if err != nil {
			return "", err
		}

		// Each model gets its own copy so that setup commands (e.g.
		// select.sh) don't clobber each other when running in parallel.
		srcDir := filepath.Join(repoDir, model.Path)
		dest := filepath.Join(r.workDir, fmt.Sprintf("model-%s", model.Name))
		if err := copyDir(srcDir, dest); err != nil {
			return "", fmt.Errorf("copying repo for model %s: %w", model.Name, err)
		}
		modelDir = dest
	}

	// Run setup commands (e.g. script/select.sh to generate cog.yaml)
	if err := r.runSetupCommands(ctx, modelDir, model); err != nil {
		return "", fmt.Errorf("running setup commands: %w", err)
	}

	cogYAMLPath := filepath.Join(modelDir, "cog.yaml")
	info, err := os.Stat(cogYAMLPath)
	if err != nil {
		return "", fmt.Errorf("no cog.yaml in %s (did setup commands run correctly?)", modelDir)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("cog.yaml in %s is empty (setup commands may have failed silently — check that tools like yq are installed)", modelDir)
	}

	// Patch cog.yaml
	sdkVersion := model.SDKVersion
	if sdkVersion == "" {
		sdkVersion = r.opts.SDKVersion
	}
	if err := patcher.Patch(filepath.Join(modelDir, "cog.yaml"), sdkVersion, model.CogYAMLOverrides); err != nil {
		return "", fmt.Errorf("patching cog.yaml: %w", err)
	}

	return modelDir, nil
}

// checkRequiredTools verifies that all tools listed in requires_tools are
// available on PATH. Returns a descriptive error listing missing tools and
// install hints when possible.
func checkRequiredTools(tools []string) error {
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("required tool(s) not found on PATH: %s", strings.Join(missing, ", "))
}

// runSetupCommands executes the model's setup commands in the model directory.
// Setup commands run after clone/copy but before cog.yaml validation and patching.
// This is used for models that need preparation steps like generating cog.yaml
// from templates (e.g. "script/select.sh dev" in replicate/cog-flux).
func (r *Runner) runSetupCommands(ctx context.Context, modelDir string, model manifest.Model) error {
	// Check required tools before running any setup commands
	if err := checkRequiredTools(model.RequiresTools); err != nil {
		return err
	}

	stdout, stderr, capture, flush := r.modelOutput(model.Name)

	for _, cmdStr := range model.Setup {
		_, _ = fmt.Fprintf(stderr, "  Running setup: %s\n", cmdStr)
		// Use bash with strict mode so any failing command in the
		// script (e.g. a missing yq binary) is caught immediately
		// rather than silently producing an empty/invalid cog.yaml.
		// We use bash (not sh) because dash does not support pipefail.
		cmd := exec.CommandContext(ctx, "bash", "-euo", "pipefail", "-c", cmdStr)
		cmd.Dir = modelDir
		cmd.Env = os.Environ()
		for k, v := range model.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
		}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			flush()
			return fmt.Errorf("setup command %q failed: %w\n%s", cmdStr, err, capture.String())
		}
	}
	flush()
	return nil
}

// cloneRepo clones a repo once and caches the result. Thread-safe.
func (r *Runner) cloneRepo(ctx context.Context, repo string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if dir, ok := r.clonedRepos[repo]; ok {
		return dir, nil
	}

	dest := filepath.Join(r.workDir, strings.ReplaceAll(repo, "/", "--"))

	// Remove if exists
	_ = os.RemoveAll(dest)

	url := fmt.Sprintf("https://github.com/%s.git", repo)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", url, dest)
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
	cmd := exec.CommandContext(ctx, r.opts.CogBinary, "build", "-t", imageTag)
	cmd.Dir = modelDir
	cmd.Env = os.Environ()
	if r.opts.SDKWheel != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("COG_SDK_WHEEL=%s", r.opts.SDKWheel))
	}
	envKeys := make([]string, 0, len(model.Env))
	for k := range model.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		v := model.Env[k]
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
	}
	extraKeys := make([]string, 0, len(extraEnv))
	for k := range extraEnv {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		v := extraEnv[k]
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Stream build output in real-time so the user can see progress,
	// while also capturing it for error reporting if the build fails.
	// In parallel mode, each line is prefixed with the model name.
	_, stderr, capture, flush := r.modelOutput(model.Name)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	err := cmd.Run()
	flush()
	if err != nil {
		// Include the last portion of build output for context.
		output := capture.String()
		const maxTail = 2000
		if len(output) > maxTail {
			output = "...\n" + output[len(output)-maxTail:]
		}
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

func (r *Runner) extractSchemaLabel(ctx context.Context, imageTag string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", imageTag, "--format", fmt.Sprintf("{{index .Config.Labels \"%s\"}}", openapiSchemaLabel))
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
	keys := make([]string, 0, len(tc.Inputs))
	for k := range tc.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := tc.Inputs[key]
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

	cmd := exec.CommandContext(ctx, r.opts.CogBinary, args...)
	cmd.Dir = modelDir
	cmd.Env = os.Environ()
	if r.opts.SDKWheel != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("COG_SDK_WHEEL=%s", r.opts.SDKWheel))
	}
	envKeys := make([]string, 0, len(model.Env))
	for k := range model.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		v := model.Env[k]
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
	for line := range strings.SplitSeq(stderr, "\n") {
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
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		info, err := d.Info()
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
