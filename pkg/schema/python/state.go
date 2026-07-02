package python

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

type ParserOptions struct {
	Source               []byte
	TargetRef            string
	Mode                 schema.Mode
	SourceDir            string
	SourcePath           string
	DisableLegacyPredict bool
}

func defaultParserOptions(source []byte, targetRef string, mode schema.Mode, sourceDir string) ParserOptions {
	return ParserOptions{
		Source:    source,
		TargetRef: targetRef,
		Mode:      mode,
		SourceDir: sourceDir,
	}
}

type parsePhase int

const (
	phaseInitial parsePhase = iota
	phaseModuleParsed
	phaseImportsCollected
	phaseModuleScopeCollected
	phaseLocalModelsCollected
	phaseImportedModelsResolved
	phaseInputRegistryCollected
	phaseTargetFound
	phaseInputsExtracted
	phaseOutputResolved
	phaseBuilt parsePhase = 1_000 // terminal sentinel; keep this last even if phases are inserted above
)

func (p parsePhase) String() string {
	switch p {
	case phaseInitial:
		return "initial"
	case phaseModuleParsed:
		return "module parsed"
	case phaseImportsCollected:
		return "imports collected"
	case phaseModuleScopeCollected:
		return "module scope collected"
	case phaseLocalModelsCollected:
		return "local models collected"
	case phaseImportedModelsResolved:
		return "imported models resolved"
	case phaseInputRegistryCollected:
		return "input registry collected"
	case phaseTargetFound:
		return "target found"
	case phaseInputsExtracted:
		return "inputs extracted"
	case phaseOutputResolved:
		return "output resolved"
	case phaseBuilt:
		return "built"
	default:
		return fmt.Sprintf("parsePhase(%d)", int(p))
	}
}

type TargetCallable struct {
	ClassName    string
	FunctionName string
	MethodName   string
	Node         *sitter.Node
	IsMethod     bool
}

type ModuleSummary struct {
	Imports    *schema.ImportContext
	Models     schema.ModelClassMap
	TypedDicts map[string]bool
	SourcePath string
}

type ParseState struct {
	Options ParserOptions
	Phase   parsePhase
	Tree    *sitter.Tree
	Root    *sitter.Node

	Imports           *schema.ImportContext
	Scope             moduleScope
	ModelCtx          *modelParseContext
	Models            schema.ModelClassMap
	LoadedModules     map[string]ModuleSummary
	Registry          *inputRegistry
	Target            *TargetCallable
	FileCtx           *pythonFileContext
	TargetFunc        *targetFunction
	Inputs            *schema.OrderedMap[string, schema.InputField]
	Output            schema.SchemaType
	SupportsStreaming bool
	ConcurrencyMax    *int
	IsAsync           bool
	OutputSet         bool
}

func newParseState(opts ParserOptions) *ParseState {
	return &ParseState{
		Options:       opts,
		Phase:         phaseInitial,
		LoadedModules: make(map[string]ModuleSummary),
	}
}

func (s *ParseState) requirePhase(required parsePhase) error {
	if s.Phase < required {
		return schema.WrapError(schema.ErrParse, fmt.Sprintf("parser phase %q requires %q", s.Phase, required), nil)
	}
	return nil
}
