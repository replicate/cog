package config

import (
	// blank import for embeds
	_ "embed"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"sigs.k8s.io/yaml"
)

const (
	defaultVersion = "1.0"
)

//go:embed data/config_schema_v1.0.json
var schemaV1 []byte

func getSchema(version string) (gojsonschema.JSONLoader, error) {

	// Default schema
	currentSchema := schemaV1

	switch version {
	case defaultVersion:
		currentSchema = schemaV1
	}

	return gojsonschema.NewStringLoader(string(currentSchema)), nil
}

func ValidateConfig(config *Config, version string) error {
	schemaLoader, err := getSchema(version)
	if err != nil {
		return err
	}
	dataLoader := gojsonschema.NewGoLoader(config)
	return ValidateSchema(schemaLoader, dataLoader)
}

func Validate(yamlConfig string, version string) error {
	j := []byte(yamlConfig)
	config, err := yaml.YAMLToJSON(j)
	if err != nil {
		return err
	}

	schemaLoader, err := getSchema(version)
	if err != nil {
		return err
	}
	dataLoader := gojsonschema.NewStringLoader(string(config))
	return ValidateSchema(schemaLoader, dataLoader)
}

func ValidateSchema(schemaLoader, dataLoader gojsonschema.JSONLoader) error {
	result, err := gojsonschema.Validate(schemaLoader, dataLoader)
	if err != nil {
		return err
	}

	if !result.Valid() {
		return toError(result)
	}
	return nil
}

func toError(result *gojsonschema.Result) error {
	err := getMostSpecificError(result.Errors())
	return err
}

const (
	jsonschemaOneOf = "number_one_of"
	jsonschemaAnyOf = "number_any_of"
)

func getDescription(err validationError) string {
	switch err.parent.Type() {
	case "invalid_type":
		if expectedType, ok := err.parent.Details()["expected"].(string); ok {
			return fmt.Sprintf("must be a %s", humanReadableType(expectedType))
		}
	case jsonschemaOneOf, jsonschemaAnyOf:
		if err.child == nil {
			return err.parent.Description()
		}
		return err.child.Description()
	}
	return err.parent.Description()
}

func humanReadableType(definition string) string {
	if definition[0:1] == "[" {
		allTypes := strings.Split(definition[1:len(definition)-1], ",")
		for i, t := range allTypes {
			allTypes[i] = humanReadableType(t)
		}
		return fmt.Sprintf(
			"%s or %s",
			strings.Join(allTypes[0:len(allTypes)-1], ", "),
			allTypes[len(allTypes)-1],
		)
	}
	if definition == "object" {
		return "mapping"
	}
	if definition == "array" {
		return "list"
	}
	return definition
}

type validationError struct {
	parent gojsonschema.ResultError
	child  gojsonschema.ResultError
}

func (err validationError) Error() string {
	description := getDescription(err)
	return fmt.Sprintf("%s %s", err.parent.Field(), description)
}

func getMostSpecificError(errors []gojsonschema.ResultError) validationError {
	mostSpecificError := 0
	for i, err := range errors {
		if specificity(err) > specificity(errors[mostSpecificError]) {
			mostSpecificError = i
			continue
		}

		if specificity(err) == specificity(errors[mostSpecificError]) {
			// Invalid type errors win in a tie-breaker for most specific field name
			if err.Type() == "invalid_type" && errors[mostSpecificError].Type() != "invalid_type" {
				mostSpecificError = i
			}
		}
	}

	if mostSpecificError+1 == len(errors) {
		return validationError{parent: errors[mostSpecificError]}
	}

	switch errors[mostSpecificError].Type() {
	case "number_one_of", "number_any_of":
		return validationError{
			parent: errors[mostSpecificError],
			child:  errors[mostSpecificError+1],
		}
	default:
		return validationError{parent: errors[mostSpecificError]}
	}
}

func specificity(err gojsonschema.ResultError) int {
	return len(strings.Split(err.Field(), "."))
}
