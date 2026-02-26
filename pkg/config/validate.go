package config

import (
	// blank import for embeds
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/xeipuuv/gojsonschema"

	"github.com/replicate/cog/pkg/requirements"
)

//go:embed data/config_schema_v1.0.json
var schemaV1 []byte

// ValidateOption configures validation behavior.
type ValidateOption func(*validateOptions)

type validateOptions struct {
	projectDir         string
	requirementsFS     fs.FS
	strictDeprecations bool
}

// WithProjectDir sets the project directory for resolving relative paths.
func WithProjectDir(dir string) ValidateOption {
	return func(o *validateOptions) {
		o.projectDir = dir
	}
}

// WithRequirementsFS sets the filesystem for reading python_requirements file.
func WithRequirementsFS(fsys fs.FS) ValidateOption {
	return func(o *validateOptions) {
		o.requirementsFS = fsys
	}
}

// WithStrictDeprecations treats deprecation warnings as errors.
func WithStrictDeprecations() ValidateOption {
	return func(o *validateOptions) {
		o.strictDeprecations = true
	}
}

// ValidateConfigFile checks a configFile for errors.
// Returns all validation errors and deprecation warnings.
// Does not mutate the input.
func ValidateConfigFile(cfg *configFile, opts ...ValidateOption) *ValidationResult {
	options := &validateOptions{}
	for _, opt := range opts {
		opt(options)
	}

	result := NewValidationResult()

	// Schema validation
	if err := validateSchema(cfg); err != nil {
		result.AddError(err)
	}

	// Semantic validation
	validateRun(cfg, result)
	validateTrain(cfg, result)
	validateBuild(cfg, options, result)
	validateEnvironment(cfg, result)
	validateConcurrency(cfg, result)

	// Check deprecated fields
	checkDeprecatedFields(cfg, result)

	// If strict deprecations, convert warnings to errors
	if options.strictDeprecations && result.HasWarnings() {
		for _, w := range result.Warnings {
			result.AddError(&w)
		}
		result.Warnings = nil
	}

	return result
}

// validateSchema validates the config against the JSON schema.
func validateSchema(cfg *configFile) error {
	schemaLoader := gojsonschema.NewStringLoader(string(schemaV1))
	dataLoader := gojsonschema.NewGoLoader(cfg)

	validationResult, err := gojsonschema.Validate(schemaLoader, dataLoader)
	if err != nil {
		return &SchemaError{Field: "(root)", Message: err.Error()}
	}

	if !validationResult.Valid() {
		// Get the most specific error
		err := getMostSpecificSchemaError(validationResult.Errors())
		return err
	}

	return nil
}

// validateRun validates the run/predict field.
// Both "run" and "predict" are accepted (predict is the legacy name),
// but they cannot both be set.
func validateRun(cfg *configFile, result *ValidationResult) {
	if cfg.Run != nil && cfg.Predict != nil && *cfg.Run != "" && *cfg.Predict != "" {
		result.AddError(&ValidationError{
			Field:   "run",
			Message: "'run' and 'predict' cannot both be set in cog.yaml; use 'run' (predict is deprecated)",
		})
		return
	}

	ref := cfg.resolvedRun()
	if ref == nil || *ref == "" {
		return
	}

	field := "run"
	if cfg.Run == nil {
		field = "predict"
	}

	if len(strings.Split(*ref, ".py:")) != 2 {
		result.AddError(&ValidationError{
			Field:   field,
			Value:   *ref,
			Message: "must be in the form 'run.py:Runner'",
		})
	}
}

// validateTrain validates the train field.
func validateTrain(cfg *configFile, result *ValidationResult) {
	if cfg.Train == nil || *cfg.Train == "" {
		return
	}

	train := *cfg.Train
	if len(strings.Split(train, ".py:")) != 2 {
		result.AddError(&ValidationError{
			Field:   "train",
			Value:   train,
			Message: "must be in the form 'train.py:Trainer'",
		})
	}
}

// validateBuild validates the build configuration.
func validateBuild(cfg *configFile, opts *validateOptions, result *ValidationResult) {
	if cfg.Build == nil {
		return
	}

	build := cfg.Build

	// Validate Python version is set and valid
	if build.PythonVersion == nil || *build.PythonVersion == "" {
		result.AddError(&ValidationError{
			Field:   "build.python_version",
			Message: "python_version is required. Add it to the build section of your cog.yaml, e.g. `python_version: \"3.13\"`",
		})
	} else {
		if err := validatePythonVersion(*build.PythonVersion); err != nil {
			result.AddError(err)
		}
	}

	// Validate mutual exclusivity of python_packages and python_requirements
	if len(build.PythonPackages) > 0 && build.PythonRequirements != nil && *build.PythonRequirements != "" {
		result.AddError(&ValidationError{
			Field:   "build",
			Message: "only one of python_packages or python_requirements can be set, not both",
		})
	}

	// Validate python_requirements file exists
	if build.PythonRequirements != nil && *build.PythonRequirements != "" {
		if err := validateRequirementsFile(*build.PythonRequirements, opts); err != nil {
			result.AddError(err)
		}
	}

	// Validate CUDA version if specified
	if build.CUDA != nil && *build.CUDA != "" {
		if err := validateCUDAVersion(*build.CUDA); err != nil {
			result.AddError(err)
		}
	}

	// Validate GPU-specific settings
	if build.GetGPU() {
		validateGPUConfig(cfg, opts, result)
	}
}

// validatePythonVersion validates the Python version string.
func validatePythonVersion(version string) error {
	version = strings.TrimSpace(version)
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return &ValidationError{
			Field:   "build.python_version",
			Value:   version,
			Message: "must include major and minor version (e.g., '3.11')",
		}
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return &ValidationError{
			Field:   "build.python_version",
			Value:   version,
			Message: "invalid major version number",
		}
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return &ValidationError{
			Field:   "build.python_version",
			Value:   version,
			Message: "invalid minor version number",
		}
	}

	if major < MinimumMajorPythonVersion || (major == MinimumMajorPythonVersion && minor < MinimumMinorPythonVersion) {
		return &ValidationError{
			Field:   "build.python_version",
			Value:   version,
			Message: fmt.Sprintf("minimum supported Python version is %d.%d", MinimumMajorPythonVersion, MinimumMinorPythonVersion),
		}
	}

	return nil
}

// validateCUDAVersion validates the CUDA version string.
func validateCUDAVersion(cudaVersion string) error {
	parts := strings.Split(cudaVersion, ".")
	if len(parts) < 2 {
		return &ValidationError{
			Field:   "build.cuda",
			Value:   cudaVersion,
			Message: "must include both major and minor versions (e.g., '11.8')",
		}
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return &ValidationError{
			Field:   "build.cuda",
			Value:   cudaVersion,
			Message: "invalid major version number",
		}
	}

	if major < MinimumMajorCudaVersion {
		return &ValidationError{
			Field:   "build.cuda",
			Value:   cudaVersion,
			Message: fmt.Sprintf("minimum supported CUDA version is %d", MinimumMajorCudaVersion),
		}
	}

	return nil
}

// validateRequirementsFile validates that the requirements file exists and is readable.
func validateRequirementsFile(reqPath string, opts *validateOptions) error {
	fullPath := reqPath
	if !strings.HasPrefix(reqPath, "/") && opts.projectDir != "" {
		fullPath = filepath.Join(opts.projectDir, reqPath)
	}

	if opts.requirementsFS != nil {
		_, err := fs.ReadFile(opts.requirementsFS, reqPath)
		if err != nil {
			return &ValidationError{
				Field:   "build.python_requirements",
				Value:   reqPath,
				Message: fmt.Sprintf("cannot read file: %v", err),
			}
		}
		return nil
	}

	// Use the real filesystem
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return &ValidationError{
			Field:   "build.python_requirements",
			Value:   reqPath,
			Message: "file does not exist",
		}
	}

	return nil
}

// validateGPUConfig validates GPU-specific configuration like CUDA/CuDNN compatibility.
func validateGPUConfig(cfg *configFile, opts *validateOptions, result *ValidationResult) {
	build := cfg.Build
	if build == nil {
		return
	}

	// If both CUDA and CuDNN are specified, check compatibility
	if build.CUDA != nil && *build.CUDA != "" && build.CuDNN != nil && *build.CuDNN != "" {
		cuda := *build.CUDA
		cudnn := *build.CuDNN
		compatibleCuDNNs := compatibleCuDNNsForCUDA(cuda)
		found := slices.Contains(compatibleCuDNNs, cudnn)
		if !found && len(compatibleCuDNNs) > 0 {
			result.AddError(&CompatibilityError{
				Component1: "CUDA",
				Version1:   cuda,
				Component2: "CuDNN",
				Version2:   cudnn,
				Message:    fmt.Sprintf("compatible CuDNN versions are: %s", strings.Join(compatibleCuDNNs, ", ")),
			})
		}
	}

	// Validate torch/tensorflow requirements if we can read them
	if build.PythonRequirements != nil && *build.PythonRequirements != "" {
		reqs := loadRequirementsForValidation(*build.PythonRequirements, opts)
		if len(reqs) > 0 {
			validateFrameworkCompatibility(cfg, reqs, result)
		}
	} else if len(build.PythonPackages) > 0 {
		validateFrameworkCompatibility(cfg, build.PythonPackages, result)
	}
}

// loadRequirementsForValidation loads requirements file contents for validation.
func loadRequirementsForValidation(reqPath string, opts *validateOptions) []string {
	fullPath := reqPath
	if !strings.HasPrefix(reqPath, "/") && opts.projectDir != "" {
		fullPath = filepath.Join(opts.projectDir, reqPath)
	}

	if opts.requirementsFS != nil {
		data, err := fs.ReadFile(opts.requirementsFS, reqPath)
		if err != nil {
			return nil
		}
		return parseRequirementsContent(string(data))
	}

	reqs, err := requirements.ReadRequirements(fullPath)
	if err != nil {
		return nil
	}
	return reqs
}

// parseRequirementsContent parses requirements.txt content into lines.
func parseRequirementsContent(content string) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		result = append(result, line)
	}
	return result
}

// validateFrameworkCompatibility checks torch/tensorflow compatibility with CUDA.
func validateFrameworkCompatibility(cfg *configFile, reqs []string, result *ValidationResult) {
	// This is a simplified version - the full logic is in Complete()
	// Here we just check for obvious errors.
	// Note: torch compatibility is checked in Complete() where it can emit warnings.
	// We only validate TensorFlow here since it has stricter requirements.

	build := cfg.Build
	if build == nil {
		return
	}

	tfVersion := findPackageVersion(reqs, "tensorflow")

	// If CUDA is specified, check TensorFlow compatibility
	if build.CUDA != nil && *build.CUDA != "" {
		cuda := *build.CUDA

		if tfVersion != "" {
			tfCUDA, _, _ := cudaFromTF(tfVersion)
			if tfCUDA != "" && !strings.HasPrefix(cuda, strings.Split(tfCUDA, ".")[0]) {
				result.AddError(&CompatibilityError{
					Component1: "TensorFlow",
					Version1:   tfVersion,
					Component2: "CUDA",
					Version2:   cuda,
					Message:    fmt.Sprintf("TensorFlow %s requires CUDA %s", tfVersion, tfCUDA),
				})
			}
		}
	}
}

// findPackageVersion finds a package version in requirements.
func findPackageVersion(reqs []string, name string) string {
	for _, req := range reqs {
		pkgName := requirements.PackageName(req)
		if pkgName == name {
			versions := requirements.Versions(req)
			if len(versions) > 0 {
				return versions[0]
			}
		}
	}
	return ""
}

// validateEnvironment validates environment variables.
func validateEnvironment(cfg *configFile, result *ValidationResult) {
	if len(cfg.Environment) == 0 {
		return
	}

	_, err := parseAndValidateEnvironment(cfg.Environment)
	if err != nil {
		result.AddError(&ValidationError{
			Field:   "environment",
			Message: err.Error(),
		})
	}
}

// validateConcurrency validates concurrency settings.
func validateConcurrency(cfg *configFile, result *ValidationResult) {
	if cfg.Concurrency == nil || cfg.Concurrency.Max == nil {
		return
	}

	maxConcurrency := *cfg.Concurrency.Max
	if maxConcurrency < 1 {
		result.AddError(&ValidationError{
			Field:   "concurrency.max",
			Value:   fmt.Sprintf("%d", maxConcurrency),
			Message: "must be at least 1",
		})
	}

	// Check Python version requirement for concurrency
	if maxConcurrency > 1 && cfg.Build != nil && cfg.Build.PythonVersion != nil {
		pyVersion := *cfg.Build.PythonVersion
		major, minor, err := splitPythonVersion(pyVersion)
		if err == nil {
			// Only check minor version if major version is the minimum (3)
			// For major > 3, any minor version would be acceptable
			if major == MinimumMajorPythonVersion && minor < MinimumMinorPythonVersionForConcurrency {
				result.AddError(&ValidationError{
					Field:   "concurrency.max",
					Value:   fmt.Sprintf("%d", maxConcurrency),
					Message: fmt.Sprintf("concurrency requires Python %d.%d or higher", MinimumMajorPythonVersion, MinimumMinorPythonVersionForConcurrency),
				})
			}
		}
	}
}

// checkDeprecatedFields checks for deprecated fields and adds warnings.
func checkDeprecatedFields(cfg *configFile, result *ValidationResult) {
	if cfg.Build == nil {
		return
	}

	if len(cfg.Build.PythonPackages) > 0 {
		result.AddWarning(DeprecationWarning{
			Field:       "build.python_packages",
			Replacement: "build.python_requirements",
			Message:     "use a requirements.txt file instead",
		})
	}

	if len(cfg.Build.PreInstall) > 0 {
		result.AddWarning(DeprecationWarning{
			Field:       "build.pre_install",
			Replacement: "build.run",
			Message:     "use build.run commands instead",
		})
	}
}

// getMostSpecificSchemaError extracts the most specific error from schema validation.
func getMostSpecificSchemaError(errors []gojsonschema.ResultError) *SchemaError {
	if len(errors) == 0 {
		return &SchemaError{Field: "(unknown)", Message: "unknown schema error"}
	}

	mostSpecific := 0
	for i, err := range errors {
		if schemaErrorSpecificity(err) > schemaErrorSpecificity(errors[mostSpecific]) {
			mostSpecific = i
		} else if schemaErrorSpecificity(err) == schemaErrorSpecificity(errors[mostSpecific]) {
			// Invalid type errors win in a tie-breaker
			if err.Type() == "invalid_type" && errors[mostSpecific].Type() != "invalid_type" {
				mostSpecific = i
			}
		}
	}

	err := errors[mostSpecific]
	field := err.Field()
	if field == "(root)" {
		field = "cog.yaml"
	}

	message := getSchemaErrorDescription(err, errors, mostSpecific)

	return &SchemaError{
		Field:   field,
		Message: message,
	}
}

// getSchemaErrorDescription generates a human-readable description for a schema error.
func getSchemaErrorDescription(err gojsonschema.ResultError, allErrors []gojsonschema.ResultError, index int) string {
	switch err.Type() {
	case "invalid_type":
		if expectedType, ok := err.Details()["expected"].(string); ok {
			return fmt.Sprintf("must be a %s", humanReadableSchemaType(expectedType))
		}
	case "number_one_of", "number_any_of":
		if index+1 < len(allErrors) {
			return allErrors[index+1].Description()
		}
	}
	return err.Description()
}

// humanReadableSchemaType converts JSON schema type names to human-readable names.
func humanReadableSchemaType(definition string) string {
	if len(definition) > 0 && definition[0] == '[' {
		allTypes := strings.Split(definition[1:len(definition)-1], ",")
		for i, t := range allTypes {
			allTypes[i] = humanReadableSchemaType(strings.TrimSpace(t))
		}
		return fmt.Sprintf("%s or %s",
			strings.Join(allTypes[0:len(allTypes)-1], ", "),
			allTypes[len(allTypes)-1])
	}
	switch definition {
	case "object":
		return "mapping"
	case "array":
		return "list"
	default:
		return definition
	}
}

// schemaErrorSpecificity returns how specific a schema error is based on field depth.
func schemaErrorSpecificity(err gojsonschema.ResultError) int {
	return len(strings.Split(err.Field(), "."))
}

// Note: The legacy Validate function is in validator.go for backwards compatibility
