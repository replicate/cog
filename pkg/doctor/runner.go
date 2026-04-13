package doctor

import (
	"bytes"
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
	Fix            bool
	ProjectDir     string
	ConfigFilename string // Config filename (defaults to "cog.yaml" if empty)
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

// HasErrors returns true if any check produced error-severity findings
// or if any check itself errored.
func (r *Result) HasErrors() bool {
	for _, cr := range r.Results {
		if cr.Err != nil {
			return true
		}
		for _, f := range cr.Findings {
			if f.Severity == SeverityError && !cr.Fixed {
				return true
			}
		}
	}
	return false
}

// Run executes all checks and optionally applies fixes.
func Run(ctx context.Context, opts RunOptions, checks []Check) (*Result, error) {
	configFilename := opts.ConfigFilename
	if configFilename == "" {
		configFilename = "cog.yaml"
	}

	checkCtx, err := buildCheckContext(ctx, opts.ProjectDir, configFilename)
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
func buildCheckContext(ctx context.Context, projectDir string, configFilename string) (*CheckContext, error) {
	ctxt := &CheckContext{
		ctx:            ctx,
		ProjectDir:     projectDir,
		ConfigFilename: configFilename,
		PythonFiles:    make(map[string]*ParsedFile),
	}

	// Load cog.yaml
	configPath := filepath.Join(projectDir, configFilename)
	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		ctxt.ConfigFile = configBytes
		// Load and validate config once — checks use ctxt.LoadResult / ctxt.LoadErr
		loadResult, loadErr := config.Load(bytes.NewReader(configBytes), projectDir)
		ctxt.LoadErr = loadErr
		if loadResult != nil {
			ctxt.LoadResult = loadResult
			ctxt.Config = loadResult.Config
		}
	}

	// Find python binary
	if pythonPath, err := exec.LookPath("python3"); err == nil {
		ctxt.PythonPath = pythonPath
	} else if pythonPath, err := exec.LookPath("python"); err == nil {
		ctxt.PythonPath = pythonPath
	}

	// Pre-parse Python files referenced in config
	if ctxt.Config != nil {
		parsePythonRef(ctxt, ctxt.Config.Predict)
		parsePythonRef(ctxt, ctxt.Config.Train)
	}

	return ctxt, nil
}

// parsePythonRef parses a predict/train reference like "predict.py:Predictor"
// and adds the parsed file to ctx.PythonFiles.
func parsePythonRef(ctxt *CheckContext, ref string) {
	if ref == "" {
		return
	}
	parts := splitPredictRef(ref)
	if parts[0] == "" {
		return
	}

	fullPath := filepath.Join(ctxt.ProjectDir, parts[0])
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(ctxt.ctx, nil, source)
	if err != nil {
		return
	}

	imports := schemaPython.CollectImports(tree.RootNode(), source)

	ctxt.PythonFiles[parts[0]] = &ParsedFile{
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
