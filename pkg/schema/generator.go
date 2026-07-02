package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Parser is a function that parses source code and extracts predictor info.
// This is defined as a type to avoid an import cycle between schema and
// schema/python. The concrete implementation is python.ParsePredictor.
//
// sourceDir is the project root directory, used for resolving cross-file
// imports (e.g. "from .types import Output"). Pass "" if unknown.
type Parser func(source []byte, predictRef string, mode Mode, sourceDir string) (*PredictorInfo, error)

// PathAwareParser is a parser that can also receive the source file path
// relative to sourceDir for package-relative import resolution.
type PathAwareParser func(source []byte, predictRef string, mode Mode, sourceDir string, sourcePath string) (*PredictorInfo, error)

// Generate produces an OpenAPI 3.0.2 JSON schema from a predict/train reference.
//
// predictRef has the format "module.py:ClassName" (e.g. "predict.py:Predictor").
// sourceDir is the project root containing the source file path from predictRef.
// mode selects predict vs train.
// parse is the parser implementation (use python.ParsePredictor).
//
// If the COG_OPENAPI_SCHEMA environment variable is set, its value is treated
// as a path to a pre-built JSON schema file. The file contents are returned
// directly and no parsing or generation takes place.
func Generate(predictRef string, sourceDir string, mode Mode, parse any) ([]byte, error) {
	// "Bring your own schema" override
	if schemaPath := os.Getenv("COG_OPENAPI_SCHEMA"); schemaPath != "" {
		data, err := os.ReadFile(schemaPath) //nolint:gosec // G703: path from trusted env var
		if err != nil {
			return nil, fmt.Errorf("COG_OPENAPI_SCHEMA: failed to read %s: %w", schemaPath, err)
		}
		return data, nil
	}

	info, err := GenerateInfo(predictRef, sourceDir, mode, parse)
	if err != nil {
		return nil, err
	}
	return GenerateOpenAPISchema(info)
}

// GenerateInfo parses predictor source and returns the extracted predictor info.
func GenerateInfo(predictRef string, sourceDir string, mode Mode, parse any) (*PredictorInfo, error) {
	filePath, className, err := parsePredictRef(predictRef)
	if err != nil {
		return nil, err
	}
	filePath, err = cleanSourceFilePath(filePath)
	if err != nil {
		return nil, err
	}

	fullPath := filePath
	if sourceDir != "" {
		fullPath = filepath.Join(sourceDir, filePath)
	}

	source, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read predictor source %s: %w", fullPath, err)
	}

	return parsePredictorInfo(parse, source, className, mode, sourceDir, filePath)
}

func cleanSourceFilePath(filePath string) (string, error) {
	clean := filepath.Clean(filePath)
	if filepath.IsAbs(clean) || pathEscapesSourceDir(clean) {
		return "", fmt.Errorf("predictor source path %q is outside source directory", filePath)
	}
	return clean, nil
}

func pathEscapesSourceDir(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

// GenerateFromSource produces an OpenAPI 3.0.2 JSON schema from Python source bytes.
//
// predictRef is the class or function name (e.g. "Predictor" or "predict").
// parse is the parser implementation (use python.ParsePredictor).
// sourceDir is the project root for resolving cross-file imports. Pass "" if unknown.
// This is the lower-level API — it does not read files or check COG_OPENAPI_SCHEMA.
func GenerateFromSource(source []byte, predictRef string, mode Mode, parse any, sourceDir string) ([]byte, error) {
	return generateFromSource(source, predictRef, mode, parse, sourceDir, "")
}

// GenerateFromSourceWithPath is like GenerateFromSource, but also provides the
// source file path to parsers that need it for package-relative imports.
func GenerateFromSourceWithPath(source []byte, predictRef string, mode Mode, parse any, sourceDir string, sourcePath string) ([]byte, error) {
	return generateFromSource(source, predictRef, mode, parse, sourceDir, sourcePath)
}

func generateFromSource(source []byte, predictRef string, mode Mode, parse any, sourceDir string, sourcePath string) ([]byte, error) {
	info, err := parsePredictorInfo(parse, source, predictRef, mode, sourceDir, sourcePath)
	if err != nil {
		return nil, err
	}
	return GenerateOpenAPISchema(info)
}

func parsePredictorInfo(parse any, source []byte, predictRef string, mode Mode, sourceDir string, sourcePath string) (*PredictorInfo, error) {
	switch parser := parse.(type) {
	case PathAwareParser:
		if sourcePath != "" {
			return parser(source, predictRef, mode, sourceDir, sourcePath)
		}
		return parser(source, predictRef, mode, sourceDir, "")
	case func([]byte, string, Mode, string, string) (*PredictorInfo, error):
		if sourcePath != "" {
			return parser(source, predictRef, mode, sourceDir, sourcePath)
		}
		return parser(source, predictRef, mode, sourceDir, "")
	case Parser:
		return parser(source, predictRef, mode, sourceDir)
	case func([]byte, string, Mode, string) (*PredictorInfo, error):
		return parser(source, predictRef, mode, sourceDir)
	default:
		return nil, fmt.Errorf("unsupported parser type %T", parse)
	}
}

// GenerateCombined produces an OpenAPI schema for both predict and train (when
// both are configured) and merges them into a single document. If only one mode
// is configured, it returns that single schema.
//
// If the COG_OPENAPI_SCHEMA environment variable is set, its value is treated
// as a path to a pre-built JSON schema file and returned directly.
func GenerateCombined(sourceDir string, predictRef string, trainRef string, parse any) ([]byte, error) {
	// "Bring your own schema" override
	if schemaPath := os.Getenv("COG_OPENAPI_SCHEMA"); schemaPath != "" {
		data, err := os.ReadFile(schemaPath) //nolint:gosec // G703: path from trusted env var
		if err != nil {
			return nil, fmt.Errorf("COG_OPENAPI_SCHEMA: failed to read %s: %w", schemaPath, err)
		}
		return data, nil
	}

	if predictRef == "" && trainRef == "" {
		return nil, fmt.Errorf("no predict or train reference provided")
	}

	// Single-mode: just generate the one schema
	if predictRef == "" {
		return Generate(trainRef, sourceDir, ModeTrain, parse)
	}
	if trainRef == "" {
		return Generate(predictRef, sourceDir, ModePredict, parse)
	}

	// Both modes: generate each and merge
	predictJSON, err := Generate(predictRef, sourceDir, ModePredict, parse)
	if err != nil {
		return nil, fmt.Errorf("predict schema: %w", err)
	}
	trainJSON, err := Generate(trainRef, sourceDir, ModeTrain, parse)
	if err != nil {
		return nil, fmt.Errorf("train schema: %w", err)
	}

	var predictSchema, trainSchema map[string]any
	if err := json.Unmarshal(predictJSON, &predictSchema); err != nil {
		return nil, fmt.Errorf("failed to parse predict schema: %w", err)
	}
	if err := json.Unmarshal(trainJSON, &trainSchema); err != nil {
		return nil, fmt.Errorf("failed to parse train schema: %w", err)
	}

	merged := MergeSchemas(predictSchema, trainSchema)
	return json.MarshalIndent(merged, "", "  ")
}

// MergeSchemas merges a predict-mode and train-mode OpenAPI schema into a single
// combined schema. The predict schema is used as the base; paths and component
// schemas from the train schema are added to it.
func MergeSchemas(predict, train map[string]any) map[string]any {
	// Merge paths
	predictPaths, _ := predict["paths"].(map[string]any)
	trainPaths, _ := train["paths"].(map[string]any)
	if predictPaths != nil && trainPaths != nil {
		for k, v := range trainPaths {
			if _, exists := predictPaths[k]; !exists {
				predictPaths[k] = v
			}
		}
	}

	// Merge component schemas
	predictComponents, _ := predict["components"].(map[string]any)
	trainComponents, _ := train["components"].(map[string]any)
	if predictComponents != nil && trainComponents != nil {
		predictSchemas, _ := predictComponents["schemas"].(map[string]any)
		trainSchemas, _ := trainComponents["schemas"].(map[string]any)
		if predictSchemas != nil && trainSchemas != nil {
			for k, v := range trainSchemas {
				if _, exists := predictSchemas[k]; !exists {
					predictSchemas[k] = v
				}
			}
		}
	}

	return predict
}

// parsePredictRef splits a predict reference like "predict.py:Predictor" into
// the file path and class/function name.
func parsePredictRef(ref string) (filePath string, name string, err error) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errInvalidPredictRef(ref)
	}
	return parts[0], parts[1], nil
}
