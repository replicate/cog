package config

import (
	"errors"
	"fmt"
)

// ConfigError is the base interface for all config errors.
// Allows callers to use errors.As to get config-specific details.
type ConfigError interface {
	error
	ConfigError() // marker method
}

// ParseError indicates the YAML file could not be parsed.
type ParseError struct {
	Filename string
	Err      error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("failed to parse %s: %v", e.Filename, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

func (e *ParseError) ConfigError() {}

// SchemaError indicates the config structure doesn't match the schema.
// For example, wrong type for a field or unknown field.
type SchemaError struct {
	Field   string
	Message string
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf("schema error in %q: %s", e.Field, e.Message)
}

func (e *SchemaError) ConfigError() {}

// ValidationError indicates a semantic validation failure.
// The config parses correctly but values are invalid.
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Value != "" {
		return fmt.Sprintf("invalid %s %q: %s", e.Field, e.Value, e.Message)
	}
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

func (e *ValidationError) ConfigError() {}

// DeprecationWarning indicates use of a deprecated field.
// This is a warning, not an error - validation still succeeds.
type DeprecationWarning struct {
	Field       string
	Replacement string
	Message     string
}

func (w *DeprecationWarning) Error() string {
	if w.Replacement != "" {
		return fmt.Sprintf("deprecated field %q: use %q instead", w.Field, w.Replacement)
	}
	return fmt.Sprintf("deprecated field %q: %s", w.Field, w.Message)
}

func (w *DeprecationWarning) ConfigError() {}

// CompatibilityError indicates an incompatible version combination.
type CompatibilityError struct {
	Component1 string
	Version1   string
	Component2 string
	Version2   string
	Message    string
}

func (e *CompatibilityError) Error() string {
	return fmt.Sprintf("%s %s is incompatible with %s %s: %s",
		e.Component1, e.Version1, e.Component2, e.Version2, e.Message)
}

func (e *CompatibilityError) ConfigError() {}

// ValidationResult holds all errors and warnings from validation.
type ValidationResult struct {
	Errors   []error
	Warnings []DeprecationWarning
}

// HasErrors returns true if there are any validation errors.
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasWarnings returns true if there are any deprecation warnings.
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// Err returns a combined error if there are any validation errors, nil otherwise.
func (r *ValidationResult) Err() error {
	if !r.HasErrors() {
		return nil
	}
	return errors.Join(r.Errors...)
}

// AddError adds a validation error.
func (r *ValidationResult) AddError(err error) {
	r.Errors = append(r.Errors, err)
}

// AddWarning adds a deprecation warning.
func (r *ValidationResult) AddWarning(w DeprecationWarning) {
	r.Warnings = append(r.Warnings, w)
}

// NewValidationResult creates an empty ValidationResult.
func NewValidationResult() *ValidationResult {
	return &ValidationResult{
		Errors:   []error{},
		Warnings: []DeprecationWarning{},
	}
}
