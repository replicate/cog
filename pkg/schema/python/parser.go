// Package python implements a tree-sitter based Python parser for extracting
// Cog predictor signatures. It walks the concrete syntax tree to extract
// imports, class definitions, function parameters with type annotations and
// default values, and Input() call keyword arguments.
//
// This parser is Python-specific. Future languages (e.g. Node.js) would get
// their own parser package under pkg/schema/.
package python

import "github.com/replicate/cog/pkg/schema"

// ParsePredictor parses Python source and extracts predictor information.
// predictRef is the class or function name (e.g. "Predictor" or "predict").
// mode controls whether we look for predict or train method.
// sourceDir is the project root for resolving cross-file imports. Pass "" if unknown.
func ParsePredictor(source []byte, predictRef string, mode schema.Mode, sourceDir string) (*schema.PredictorInfo, error) {
	state := newParseState(source, predictRef, mode, sourceDir)
	for state.phase != phaseDone {
		if err := state.step(); err != nil {
			return nil, err
		}
	}
	return state.result(), nil
}
