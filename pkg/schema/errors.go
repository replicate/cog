package schema

import "fmt"

// SchemaError represents errors during schema generation.
type SchemaError struct {
	Kind    SchemaErrorKind
	Message string
}

func (e *SchemaError) Error() string { return e.Message }

// SchemaErrorKind classifies schema generation errors.
type SchemaErrorKind int

const (
	ErrParse SchemaErrorKind = iota
	ErrPredictorNotFound
	ErrMethodNotFound
	ErrMissingReturnType
	ErrMissingTypeAnnotation
	ErrUnsupportedType
	ErrDefaultFactoryNotSupported
	ErrInvalidConstraint
	ErrInvalidPredictRef
	ErrOptionalOutput
	ErrConcatIteratorNotStr
	ErrChoicesNotResolvable
	ErrDefaultNotResolvable
	ErrUnresolvableType
	ErrOther
)

// NewError creates a SchemaError with the given kind and message.
func NewError(kind SchemaErrorKind, msg string) *SchemaError {
	return &SchemaError{Kind: kind, Message: msg}
}

// WrapError creates a SchemaError, appending the inner error's message if non-nil.
func WrapError(kind SchemaErrorKind, msg string, inner error) *SchemaError {
	if inner != nil {
		return &SchemaError{Kind: kind, Message: fmt.Sprintf("%s: %s", msg, inner.Error())}
	}
	return &SchemaError{Kind: kind, Message: msg}
}

func errParse(msg string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{Kind: ErrParse, Message: fmt.Sprintf("failed to parse Python source: %s", msg)}
}

func errPredictorNotFound(name string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{Kind: ErrPredictorNotFound, Message: fmt.Sprintf("predictor not found: %s", name)}
}

func errMethodNotFound(class, method string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{Kind: ErrMethodNotFound, Message: fmt.Sprintf("%s method not found on %s", method, class)}
}

func errMissingReturnType(method string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{Kind: ErrMissingReturnType, Message: fmt.Sprintf("missing return type annotation on %s", method)}
}

func errMissingTypeAnnotation(method, param string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{Kind: ErrMissingTypeAnnotation, Message: fmt.Sprintf("missing type annotation for parameter '%s' on %s", param, method)}
}

func errUnsupportedType(msg string) error {
	return &SchemaError{Kind: ErrUnsupportedType, Message: fmt.Sprintf("unsupported type: %s", msg)}
}

func errDefaultFactoryNotSupported(param string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{
		Kind:    ErrDefaultFactoryNotSupported,
		Message: fmt.Sprintf("default_factory is not supported in Input() — use a literal default value instead (parameter '%s')", param),
	}
}

func errInvalidPredictRef(ref string) error {
	return &SchemaError{
		Kind:    ErrInvalidPredictRef,
		Message: fmt.Sprintf("invalid predict reference '%s' — expected format: file.py:ClassName or file.py:function_name", ref),
	}
}

func errOptionalOutput() error {
	return &SchemaError{Kind: ErrOptionalOutput, Message: "unsupported output type: Optional is not allowed as a return type"}
}

func errConcatIteratorNotStr(got string) error {
	return &SchemaError{Kind: ErrConcatIteratorNotStr, Message: fmt.Sprintf("ConcatenateIterator element type must be str, got %s", got)}
}

func errChoicesNotResolvable(param string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{
		Kind:    ErrChoicesNotResolvable,
		Message: fmt.Sprintf("choices for parameter '%s' cannot be statically resolved — use a literal list instead (e.g. choices=[\"a\", \"b\"])", param),
	}
}

func errUnresolvableImportedType(name, module string) error {
	return &SchemaError{
		Kind: ErrUnresolvableType,
		Message: fmt.Sprintf(
			"cannot resolve output type '%s' (imported from '%s') — "+
				"external types cannot be statically analyzed. "+
				"Define it as a BaseModel subclass in your predict file, or provide a .pyi stub",
			name, module),
	}
}

func errUnresolvableType(name string) error {
	return &SchemaError{
		Kind: ErrUnresolvableType,
		Message: fmt.Sprintf(
			"cannot resolve output type '%s' — "+
				"it is not a primitive type (str, int, float, bool, Path) "+
				"and no BaseModel definition was found in the predict file",
			name),
	}
}

func errDefaultNotResolvable(param, expr string) error { //nolint:unused // used by generator.go (not yet written)
	return &SchemaError{
		Kind: ErrDefaultNotResolvable,
		Message: fmt.Sprintf(
			"default value for parameter '%s' cannot be statically resolved: `%s`. "+
				"Defaults must be literals (string, int, float, bool, None, list) or Input() calls.", param, expr),
	}
}
