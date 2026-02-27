package schema

import (
	"fmt"
	"os"
	"strings"
)

// Parser is a function that parses source code and extracts predictor info.
// This is defined as a type to avoid an import cycle between schema and
// schema/python. The concrete implementation is python.ParsePredictor.
type Parser func(source []byte, predictRef string, mode Mode) (*PredictorInfo, error)

// Generate produces an OpenAPI 3.0.2 JSON schema from a predict/train reference.
//
// predictRef has the format "module.py:ClassName" (e.g. "predict.py:Predictor").
// sourceDir is the directory containing the source file.
// mode selects predict vs train.
// parse is the parser implementation (use python.ParsePredictor).
//
// If the COG_OPENAPI_SCHEMA environment variable is set, its value is treated
// as a path to a pre-built JSON schema file. The file contents are returned
// directly and no parsing or generation takes place.
func Generate(predictRef string, sourceDir string, mode Mode, parse Parser) ([]byte, error) {
	// "Bring your own schema" override
	if schemaPath := os.Getenv("COG_OPENAPI_SCHEMA"); schemaPath != "" {
		data, err := os.ReadFile(schemaPath)
		if err != nil {
			return nil, fmt.Errorf("COG_OPENAPI_SCHEMA: failed to read %s: %w", schemaPath, err)
		}
		return data, nil
	}

	filePath, className, err := parsePredictRef(predictRef)
	if err != nil {
		return nil, err
	}

	fullPath := filePath
	if sourceDir != "" {
		fullPath = sourceDir + "/" + filePath
	}

	source, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read predictor source %s: %w", fullPath, err)
	}

	return GenerateFromSource(source, className, mode, parse)
}

// GenerateFromSource produces an OpenAPI 3.0.2 JSON schema from Python source bytes.
//
// predictRef is the class or function name (e.g. "Predictor" or "predict").
// parse is the parser implementation (use python.ParsePredictor).
// This is the lower-level API — it does not read files or check COG_OPENAPI_SCHEMA.
func GenerateFromSource(source []byte, predictRef string, mode Mode, parse Parser) ([]byte, error) {
	info, err := parse(source, predictRef, mode)
	if err != nil {
		return nil, err
	}
	return GenerateOpenAPISchema(info)
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
