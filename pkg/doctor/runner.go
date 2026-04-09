package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/config"
	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

// RunOptions configures a doctor run.
type RunOptions struct {
	Fix        bool
	ProjectDir string
}

// CheckResult holds the outcome of running a single check.
type CheckResult struct {
	Check    Check
	Findings []Finding
	Fixed    bool  // True if --fix was passed and Fix() succeeded
	Err      error // Non-nil if the check itself errored
}

// Result holds the outcome of a full doctor run.
type Result struct {
	Results []CheckResult
}

// HasErrors returns true if any check produced error-severity findings.
func (r *Result) HasErrors() bool {
	for _, cr := range r.Results {
		for _, f := range cr.Findings {
			if f.Severity == SeverityError && !cr.Fixed {
				return true
			}
		}
	}
	return false
}

// Run executes all checks and optionally applies fixes.
func Run(_ context.Context, opts RunOptions, checks []Check) (*Result, error) {
	checkCtx, err := buildCheckContext(opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	result := &Result{}

	for _, check := range checks {
		cr := CheckResult{Check: check}

		findings, err := check.Check(checkCtx)
		if err != nil {
			cr.Err = err
			result.Results = append(result.Results, cr)
			continue
		}

		cr.Findings = findings

		if opts.Fix && len(findings) > 0 {
			fixErr := check.Fix(checkCtx, findings)
			if fixErr == nil {
				cr.Fixed = true
			} else if !errors.Is(fixErr, ErrNoAutoFix) {
				cr.Err = fixErr
			}
		}

		result.Results = append(result.Results, cr)
	}

	return result, nil
}

// buildCheckContext constructs the shared context for all checks.
func buildCheckContext(projectDir string) (*CheckContext, error) {
	ctx := &CheckContext{
		ProjectDir:  projectDir,
		PythonFiles: make(map[string]*ParsedFile),
	}

	// Load cog.yaml
	configPath := filepath.Join(projectDir, "cog.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		ctx.ConfigFile = configBytes
		// Try to load and validate config
		f, err := os.Open(configPath)
		if err == nil {
			defer f.Close()
			loadResult, err := config.Load(f, projectDir)
			if err == nil {
				ctx.Config = loadResult.Config
			}
		}
	}

	// Find python binary
	if pythonPath, err := exec.LookPath("python3"); err == nil {
		ctx.PythonPath = pythonPath
	} else if pythonPath, err := exec.LookPath("python"); err == nil {
		ctx.PythonPath = pythonPath
	}

	// Pre-parse Python files referenced in config
	if ctx.Config != nil {
		parsePythonRef(ctx, ctx.Config.Predict)
		parsePythonRef(ctx, ctx.Config.Train)
	}

	return ctx, nil
}

// parsePythonRef parses a predict/train reference like "predict.py:Predictor"
// and adds the parsed file to ctx.PythonFiles.
func parsePythonRef(ctx *CheckContext, ref string) {
	if ref == "" {
		return
	}
	parts := splitPredictRef(ref)
	if parts[0] == "" {
		return
	}

	fullPath := filepath.Join(ctx.ProjectDir, parts[0])
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return
	}

	imports := schemaPython.CollectImports(tree.RootNode(), source)

	ctx.PythonFiles[parts[0]] = &ParsedFile{
		Path:    parts[0],
		Source:  source,
		Tree:    tree,
		Imports: imports,
	}
}

// splitPredictRef splits "predict.py:Predictor" into ["predict.py", "Predictor"].
func splitPredictRef(ref string) [2]string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return [2]string{ref[:i], ref[i+1:]}
		}
	}
	return [2]string{ref, ""}
}
