package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

// ConfigPredictRefCheck verifies that the predict field in cog.yaml
// points to a file and class that actually exist.
type ConfigPredictRefCheck struct{}

func (c *ConfigPredictRefCheck) Name() string        { return "config-predict-ref" }
func (c *ConfigPredictRefCheck) Group() Group        { return GroupConfig }
func (c *ConfigPredictRefCheck) Description() string { return "Predict reference" }

func (c *ConfigPredictRefCheck) Check(ctx *CheckContext) ([]Finding, error) {
	// Get predict ref from config
	predictRef := ""
	if ctx.Config != nil {
		predictRef = ctx.Config.Predict
	}
	if predictRef == "" {
		return nil, nil // No predict field — nothing to check
	}

	pyFile, className := splitPredictRef(predictRef)

	if pyFile == "" || className == "" {
		return []Finding{{
			Severity:    SeverityError,
			Message:     fmt.Sprintf("predict reference %q must be in the form 'file.py:ClassName'", predictRef),
			Remediation: `Set predict to "predict.py:Predictor" in cog.yaml`,
			File:        "cog.yaml",
		}}, nil
	}

	// Check file exists
	fullPath := filepath.Join(ctx.ProjectDir, pyFile)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return []Finding{{
			Severity:    SeverityError,
			Message:     fmt.Sprintf("%s not found", pyFile),
			Remediation: fmt.Sprintf("Create %s or update the predict field in cog.yaml", pyFile),
			File:        "cog.yaml",
		}}, nil
	}

	// Use cached parse tree if available, otherwise parse from disk
	var rootNode *sitter.Node
	var source []byte

	if pf, ok := ctx.PythonFiles[pyFile]; ok {
		rootNode = pf.Tree.RootNode()
		source = pf.Source
	} else {
		var err error
		source, err = os.ReadFile(fullPath)
		if err != nil {
			return []Finding{{
				Severity: SeverityError,
				Message:  fmt.Sprintf("cannot read %s: %v", pyFile, err),
				File:     pyFile,
			}}, nil
		}

		parser := sitter.NewParser()
		parser.SetLanguage(python.GetLanguage())
		tree, err := parser.ParseCtx(ctx.ctx, nil, source)
		if err != nil {
			return []Finding{{
				Severity: SeverityError,
				Message:  fmt.Sprintf("cannot parse %s: %v", pyFile, err),
				File:     pyFile,
			}}, nil
		}
		rootNode = tree.RootNode()
	}

	if !hasClassDefinition(rootNode, source, className) {
		// List available classes to help the user
		classes := listClassNames(rootNode, source)
		msg := fmt.Sprintf("class %q not found in %s", className, pyFile)
		if len(classes) > 0 {
			msg += fmt.Sprintf("; found: %s", strings.Join(classes, ", "))
		}
		return []Finding{{
			Severity:    SeverityError,
			Message:     msg,
			Remediation: fmt.Sprintf("Add class %s to %s or update the predict field in cog.yaml", className, pyFile),
			File:        pyFile,
		}}, nil
	}

	return nil, nil
}

func (c *ConfigPredictRefCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}

// hasClassDefinition checks whether a class with the given name exists at module level.
func hasClassDefinition(root *sitter.Node, source []byte, name string) bool {
	for _, child := range schemaPython.NamedChildren(root) {
		classNode := schemaPython.UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil && schemaPython.Content(nameNode, source) == name {
			return true
		}
	}
	return false
}

// listClassNames returns the names of all top-level classes in the file.
func listClassNames(root *sitter.Node, source []byte) []string {
	var names []string
	for _, child := range schemaPython.NamedChildren(root) {
		classNode := schemaPython.UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil {
			names = append(names, schemaPython.Content(nameNode, source))
		}
	}
	return names
}
