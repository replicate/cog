package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

var (
	topLevelPredictKeyPattern = regexp.MustCompile(`(?m)^predict\s*:`)
	topLevelRunKeyPattern     = regexp.MustCompile(`(?m)^run\s*:`)
	predictRefPattern         = regexp.MustCompile(`(?m)^predict:\s*["']?predict\.py:Predictor["']?\s*(?:#.*)?$`)
)

// PredictToRunMigrationCheck detects deprecated predict interface names and
// migrates the common starter-project shape to run interface names.
type PredictToRunMigrationCheck struct{}

func (c *PredictToRunMigrationCheck) Name() string { return "predict-to-run-migration" }
func (c *PredictToRunMigrationCheck) Group() Group { return GroupConfig }
func (c *PredictToRunMigrationCheck) Description() string {
	return "Deprecated predict interface names"
}

func (c *PredictToRunMigrationCheck) Check(ctx *CheckContext) ([]Finding, error) {
	var findings []Finding
	if topLevelPredictKeyPattern.Match(ctx.ConfigFile) || predictRefPattern.Match(ctx.ConfigFile) {
		findings = append(findings, Finding{
			Severity:    SeverityWarning,
			Message:     "predict in cog.yaml is deprecated; use run with run.py:Runner",
			Remediation: "Run cog doctor --fix to migrate predict: to run:",
			File:        ctx.ConfigFilename,
		})
	}

	if predictRefPattern.Match(ctx.ConfigFile) {
		if pf, ok := predictMigrationFile(ctx); ok && hasLegacyPredictPythonNames(pf) {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Message:     "predict.py uses deprecated Predictor/BasePredictor/predict() names",
				Remediation: "Run cog doctor --fix to migrate to Runner/BaseRunner/run()",
				File:        "predict.py",
			})
		}
	}

	return findings, nil
}

func (c *PredictToRunMigrationCheck) Fix(ctx *CheckContext, findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}

	if err := preflightPredictToRunCollisions(ctx); err != nil {
		return err
	}
	pf, ok := predictMigrationFile(ctx)
	if !ok {
		return fmt.Errorf("cannot migrate predict.py to run.py because predict.py was not found")
	}
	if !hasLegacyPredictPythonNames(pf) {
		return fmt.Errorf("cannot migrate predict.py because no legacy Predictor/BasePredictor/predict() names were found")
	}
	edits, err := predictToRunMigrationEdits(pf)
	if err != nil {
		return err
	}
	source := applyEdits(pf.Source, edits)

	configPath := filepath.Join(ctx.ProjectDir, ctx.ConfigFilename)
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	configBytes = predictRefPattern.ReplaceAll(configBytes, []byte(`run: "run.py:Runner"`))
	if err := os.WriteFile(configPath, configBytes, 0o644); err != nil {
		return err
	}

	oldPath := filepath.Join(ctx.ProjectDir, "predict.py")
	newPath := filepath.Join(ctx.ProjectDir, "run.py")
	if err := os.WriteFile(newPath, source, 0o644); err != nil {
		return err
	}
	if err := os.Remove(oldPath); err != nil {
		return err
	}

	ctx.ConfigFile = configBytes
	if ctx.Config != nil {
		ctx.Config.Predict = "run.py:Runner"
	}
	if ctx.LoadResult != nil && ctx.LoadResult.Config != nil {
		ctx.LoadResult.Config.Predict = "run.py:Runner"
		warnings := ctx.LoadResult.Warnings[:0]
		for _, warning := range ctx.LoadResult.Warnings {
			if warning.Field != "predict" {
				warnings = append(warnings, warning)
			}
		}
		ctx.LoadResult.Warnings = warnings
	}
	delete(ctx.PythonFiles, "predict.py")
	parsePythonRef(ctx, "run.py:Runner")

	return nil
}

func hasLegacyPredictPythonNames(pf *ParsedFile) bool {
	root := pf.Tree.RootNode()
	return findMigrationClassByName(root, pf.Source, "Predictor") != nil
}

func preflightPredictToRunCollisions(ctx *CheckContext) error {
	if topLevelRunKeyPattern.Match(ctx.ConfigFile) {
		return fmt.Errorf("automatic migration cannot run when run is already set")
	}
	if !predictRefPattern.Match(ctx.ConfigFile) {
		return fmt.Errorf("Manual migration required: automatic migration only supports predict.py:Predictor")
	}
	candidate := filepath.Join(ctx.ProjectDir, "run.py")
	if _, err := os.Stat(candidate); err == nil {
		return fmt.Errorf("cannot migrate predict.py to run.py because run.py already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func predictMigrationFile(ctx *CheckContext) (*ParsedFile, bool) {
	if pf, ok := ctx.PythonFiles["predict.py"]; ok && pf != nil {
		return pf, true
	}
	path := "predict.py"
	source, err := os.ReadFile(filepath.Join(ctx.ProjectDir, path))
	if err != nil {
		return nil, false
	}
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(ctx.ctx, nil, source)
	if err != nil {
		return nil, false
	}
	pf := &ParsedFile{
		Path:    path,
		Source:  source,
		Tree:    tree,
		Imports: schemaPython.CollectImports(tree.RootNode(), source),
	}
	ctx.PythonFiles[path] = pf
	return pf, true
}

func predictToRunMigrationEdits(pf *ParsedFile) ([]byteEdit, error) {
	root := pf.Tree.RootNode()
	classes := findMigrationClassesByName(root, pf.Source, "Predictor")
	if len(classes) != 1 {
		return nil, fmt.Errorf("Manual migration required: automatic migration only supports a single Predictor class")
	}
	classNode := classes[0]
	if targetClassUsesAliasedBasePredictor(classNode, pf) {
		return nil, fmt.Errorf("Manual migration required: automatic migration does not support aliased BasePredictor inheritance")
	}
	predictMethods := findMigrationMethodsByName(classNode, pf.Source, "predict")
	if len(predictMethods) != 1 || findMigrationMethodByName(classNode, pf.Source, "run") != nil {
		return nil, fmt.Errorf("Manual migration required: automatic migration only supports a single Predictor class with a single predict method")
	}

	var edits []byteEdit
	if nameNode := classNode.ChildByFieldName("name"); nameNode != nil {
		edits = append(edits, replaceNode(nameNode, []byte("Runner")))
	}
	if nameNode := predictMethods[0].ChildByFieldName("name"); nameNode != nil {
		edits = append(edits, replaceNode(nameNode, []byte("run")))
	}
	targetBaseNodes := collectTargetBasePredictorNodes(classNode, pf.Source)
	if len(targetBaseNodes) > 0 && len(collectCogImportIdentifiers(root, pf.Source, "BasePredictor")) == 0 {
		return nil, fmt.Errorf("Manual migration required: automatic migration only supports BasePredictor imported from cog")
	}
	if len(targetBaseNodes) > 0 && hasUnsupportedBasePredictorImport(root, pf.Source) {
		return nil, fmt.Errorf("Manual migration required: automatic migration only supports unambiguous BasePredictor imports from cog")
	}
	for _, node := range targetBaseNodes {
		edits = append(edits, replaceNode(node, []byte("BaseRunner")))
	}
	edits = append(edits, baseRunnerImportEdits(root, pf.Source, targetBaseNodes)...)
	return edits, nil
}

func replaceNode(node *sitter.Node, replacement []byte) byteEdit {
	return byteEdit{start: node.StartByte(), end: node.EndByte(), replacement: replacement}
}

func findMigrationClassByName(root *sitter.Node, source []byte, name string) *sitter.Node {
	classes := findMigrationClassesByName(root, source, name)
	if len(classes) == 0 {
		return nil
	}
	return classes[0]
}

func findMigrationClassesByName(root *sitter.Node, source []byte, name string) []*sitter.Node {
	var classes []*sitter.Node
	for _, child := range schemaPython.NamedChildren(root) {
		classNode := schemaPython.UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil && schemaPython.Content(nameNode, source) == name {
			classes = append(classes, classNode)
		}
	}
	return classes
}

func findMigrationMethodByName(classNode *sitter.Node, source []byte, name string) *sitter.Node {
	methods := findMigrationMethodsByName(classNode, source, name)
	if len(methods) == 0 {
		return nil
	}
	return methods[0]
}

func findMigrationMethodsByName(classNode *sitter.Node, source []byte, name string) []*sitter.Node {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var methods []*sitter.Node
	for _, child := range schemaPython.NamedChildren(body) {
		funcNode := schemaPython.UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil && schemaPython.Content(nameNode, source) == name {
			methods = append(methods, funcNode)
		}
	}
	return methods
}

func collectTargetBasePredictorNodes(classNode *sitter.Node, source []byte) []*sitter.Node {
	superclasses := classNode.ChildByFieldName("superclasses")
	if superclasses == nil {
		return nil
	}
	return collectMigrationIdentifiers(superclasses, source, "BasePredictor")
}

func targetClassUsesAliasedBasePredictor(classNode *sitter.Node, pf *ParsedFile) bool {
	superclasses := classNode.ChildByFieldName("superclasses")
	if superclasses == nil {
		return false
	}
	basePredictorAliases := collectBasePredictorImportAliases(pf.Tree.RootNode(), pf.Source)
	for _, node := range collectIdentifiers(superclasses) {
		name := schemaPython.Content(node, pf.Source)
		if basePredictorAliases[name] {
			return true
		}
	}
	return false
}

func baseRunnerImportEdits(root *sitter.Node, source []byte, targetBaseNodes []*sitter.Node) []byteEdit {
	basePredictorImports := collectCogImportIdentifiers(root, source, "BasePredictor")
	if len(basePredictorImports) == 0 {
		return nil
	}
	basePredictorUsedOutsideImportAndTargetBase := basePredictorUsedOutside(root, source, append(targetBaseNodes, basePredictorImports...))
	if len(targetBaseNodes) == 0 && basePredictorUsedOutsideImportAndTargetBase {
		return nil
	}
	if basePredictorUsedOutsideImportAndTargetBase {
		edits := make([]byteEdit, 0, len(basePredictorImports))
		for _, node := range basePredictorImports {
			edits = append(edits, byteEdit{start: node.EndByte(), end: node.EndByte(), replacement: []byte(", BaseRunner")})
		}
		return edits
	}

	edits := make([]byteEdit, 0, len(basePredictorImports))
	for _, node := range basePredictorImports {
		edits = append(edits, replaceNode(node, []byte("BaseRunner")))
	}
	return edits
}

func collectCogImportIdentifiers(root *sitter.Node, source []byte, name string) []*sitter.Node {
	var nodes []*sitter.Node
	if root.Type() == "import_from_statement" {
		moduleNode := root.ChildByFieldName("module_name")
		if moduleNode != nil && isCogBaseModule(schemaPython.Content(moduleNode, source)) {
			nodes = append(nodes, collectImportIdentifierNodes(root, moduleNode, source, name)...)
		}
	}
	for _, child := range schemaPython.NamedChildren(root) {
		nodes = append(nodes, collectCogImportIdentifiers(child, source, name)...)
	}
	return nodes
}

func hasUnsupportedBasePredictorImport(root *sitter.Node, source []byte) bool {
	if root.Type() == "import_from_statement" {
		moduleNode := root.ChildByFieldName("module_name")
		if moduleNode != nil && !isCogBaseModule(schemaPython.Content(moduleNode, source)) {
			if len(collectImportIdentifierNodes(root, moduleNode, source, "BasePredictor")) > 0 {
				return true
			}
		}
	}
	for _, child := range schemaPython.NamedChildren(root) {
		if hasUnsupportedBasePredictorImport(child, source) {
			return true
		}
	}
	return false
}

func collectImportIdentifierNodes(node *sitter.Node, moduleNode *sitter.Node, source []byte, name string) []*sitter.Node {
	if node.StartByte() == moduleNode.StartByte() && node.EndByte() == moduleNode.EndByte() {
		return nil
	}
	var nodes []*sitter.Node
	if node.Type() == "dotted_name" && schemaPython.Content(node, source) == name {
		nodes = append(nodes, node)
	}
	for _, child := range schemaPython.AllChildren(node) {
		nodes = append(nodes, collectImportIdentifierNodes(child, moduleNode, source, name)...)
	}
	return nodes
}

func isCogBaseModule(module string) bool {
	return module == "cog" || module == "cog.predictor"
}

func collectBasePredictorImportAliases(root *sitter.Node, source []byte) map[string]bool {
	aliases := make(map[string]bool)
	if root.Type() == "import_from_statement" {
		moduleNode := root.ChildByFieldName("module_name")
		if moduleNode != nil && isCogBaseModule(schemaPython.Content(moduleNode, source)) {
			for _, node := range collectAliasedImportNodes(root) {
				nameNode := node.ChildByFieldName("name")
				aliasNode := node.ChildByFieldName("alias")
				if nameNode != nil && aliasNode != nil && schemaPython.Content(nameNode, source) == "BasePredictor" {
					aliases[schemaPython.Content(aliasNode, source)] = true
				}
			}
		}
	}
	for _, child := range schemaPython.NamedChildren(root) {
		for alias := range collectBasePredictorImportAliases(child, source) {
			aliases[alias] = true
		}
	}
	return aliases
}

func collectAliasedImportNodes(node *sitter.Node) []*sitter.Node {
	var nodes []*sitter.Node
	if node.Type() == "aliased_import" {
		nodes = append(nodes, node)
	}
	for _, child := range schemaPython.NamedChildren(node) {
		nodes = append(nodes, collectAliasedImportNodes(child)...)
	}
	return nodes
}

func basePredictorUsedOutside(node *sitter.Node, source []byte, ignored []*sitter.Node) bool {
	if node.Type() == "identifier" && schemaPython.Content(node, source) == "BasePredictor" && !nodeInList(node, ignored) {
		return true
	}
	for _, child := range schemaPython.NamedChildren(node) {
		if basePredictorUsedOutside(child, source, ignored) {
			return true
		}
	}
	return false
}

func nodeInList(node *sitter.Node, nodes []*sitter.Node) bool {
	for _, candidate := range nodes {
		if node.StartByte() == candidate.StartByte() && node.EndByte() == candidate.EndByte() {
			return true
		}
	}
	return false
}

func collectMigrationIdentifiers(node *sitter.Node, source []byte, name string) []*sitter.Node {
	var nodes []*sitter.Node
	if node.Type() == "identifier" && schemaPython.Content(node, source) == name {
		nodes = append(nodes, node)
	}
	for _, child := range schemaPython.NamedChildren(node) {
		nodes = append(nodes, collectMigrationIdentifiers(child, source, name)...)
	}
	return nodes
}

func collectIdentifiers(node *sitter.Node) []*sitter.Node {
	var nodes []*sitter.Node
	if node.Type() == "identifier" {
		nodes = append(nodes, node)
	}
	for _, child := range schemaPython.NamedChildren(node) {
		nodes = append(nodes, collectIdentifiers(child)...)
	}
	return nodes
}
