package doctor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	defer checkCtx.Close()

	result := &Result{}

	for _, check := range checks {
		// Short-circuit if the caller has canceled the context (e.g. Ctrl-C).
		// Checks themselves may also honor ctx.ctx internally, but we check here
		// so cancellation is respected between checks even for fast, synchronous
		// checks that never inspect the context.
		if err := checkCtx.ctx.Err(); err != nil {
			return result, err
		}

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
	cc := &CheckContext{
		ctx:            ctx,
		ProjectDir:     projectDir,
		ConfigFilename: configFilename,
		PythonFiles:    make(map[string]*ParsedFile),
	}

	// Load cog.yaml
	configPath := filepath.Join(projectDir, configFilename)
	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		cc.ConfigFile = configBytes
		// Load and validate config once — checks use cc.LoadResult / cc.LoadErr
		loadResult, loadErr := config.Load(bytes.NewReader(configBytes), projectDir)
		cc.LoadErr = loadErr
		if loadResult != nil {
			cc.LoadResult = loadResult
			cc.Config = loadResult.Config
		}
	}

	// Find python binary
	if pythonPath, err := exec.LookPath("python3"); err == nil {
		cc.PythonPath = pythonPath
	} else if pythonPath, err := exec.LookPath("python"); err == nil {
		cc.PythonPath = pythonPath
	}

	// Pre-parse Python files referenced in config
	if cc.Config != nil {
		parsePythonRef(cc, cc.Config.Predict)
		parsePythonRef(cc, cc.Config.Train)
	}

	return cc, nil
}

// parsePythonRef parses a predict/train reference like "predict.py:Predictor"
// and adds the parsed file to ctx.PythonFiles.
func parsePythonRef(cc *CheckContext, ref string) {
	if ref == "" {
		return
	}
	pyFile, _ := splitPredictRef(ref)
	if pyFile == "" {
		return
	}

	fullPath := filepath.Join(cc.ProjectDir, pyFile)
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(cc.ctx, nil, source)
	if err != nil {
		return
	}

	imports := schemaPython.CollectImports(tree.RootNode(), source)

	cc.PythonFiles[pyFile] = &ParsedFile{
		Path:    pyFile,
		Source:  source,
		Tree:    tree,
		Imports: imports,
	}
}

// splitPredictRef splits "predict.py:Predictor" into ("predict.py", "Predictor").
// If ref has no colon, class is "". Handles Windows-style refs by splitting at
// the last colon (so "C:\path\predict.py:Predictor" → ("C:\path\predict.py", "Predictor")).
func splitPredictRef(ref string) (file, class string) {
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}
