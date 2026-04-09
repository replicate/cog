package doctor

import (
	"errors"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/schema"
)

// ErrNoAutoFix is returned by Fix() for detect-only checks.
var ErrNoAutoFix = errors.New("no auto-fix available for this check")

// Severity of a finding.
type Severity int

const (
	SeverityError   Severity = iota // Must fix -- will cause build/predict failures
	SeverityWarning                 // Should fix -- deprecated patterns, future breakage
	SeverityInfo                    // Informational -- suggestions, best practices
)

// String returns the human-readable name of the severity level.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// Group categorizes checks for display purposes.
type Group string

const (
	GroupConfig      Group = "Config"
	GroupPython      Group = "Python"
	GroupEnvironment Group = "Environment"
)

// Finding represents a single problem detected by a check.
type Finding struct {
	Severity    Severity
	Message     string // What's wrong
	Remediation string // How to fix it
	File        string // Optional: file path where the issue was found
	Line        int    // Optional: line number (1-indexed, 0 means unknown)
}

// Check is the interface every doctor rule implements.
type Check interface {
	Name() string
	Group() Group
	Description() string
	Check(ctx *CheckContext) ([]Finding, error)
	Fix(ctx *CheckContext, findings []Finding) error
}

// ParsedFile holds tree-sitter parse results for a Python file.
type ParsedFile struct {
	Path    string                // Relative path from project root
	Source  []byte                // Raw file contents
	Tree    *sitter.Tree          // Tree-sitter parse tree
	Imports *schema.ImportContext // Collected imports
}

// CheckContext provides checks with access to project state.
// Built once by the runner and passed to every check.
type CheckContext struct {
	ProjectDir     string
	ConfigFilename string                 // Config filename (e.g. "cog.yaml")
	Config         *config.Config         // Parsed cog.yaml (nil if parsing failed)
	ConfigFile     []byte                 // Raw cog.yaml bytes (available even if parsing failed)
	LoadResult     *config.LoadResult     // Non-nil if config loaded successfully
	LoadErr        error                  // Non-nil if config loading failed
	PythonFiles    map[string]*ParsedFile // Pre-parsed Python files keyed by relative path
	PythonPath     string                 // Path to python binary (empty if not found)
}
