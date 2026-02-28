package schema

import "fmt"

// SchemaType is a recursive algebraic data type representing any type that
// can appear in a Cog predictor's output (or, in the future, input) position.
//
// It replaces the flat OutputType/PrimitiveType system with a composable
// type tree that can represent dict[str, list[int]], nested BaseModel
// subclasses, TypedDicts, and types resolved from .pyi stubs — all without
// running Python.
type SchemaType struct {
	Kind SchemaTypeKind

	// Primitive: for Kind=SchemaPrimitive — one of the base scalar types.
	Primitive PrimitiveType

	// Array: for Kind=SchemaArray — the element type.
	Items *SchemaType

	// Dict: for Kind=SchemaDict — key and value types.
	// KeyType is always string in JSON Schema, but we track it for completeness.
	KeyType   *SchemaType
	ValueType *SchemaType

	// Object: for Kind=SchemaObject — named fields with types and defaults.
	Fields *OrderedMap[string, SchemaField]

	// Iterator/ConcatIterator: for Kind=SchemaIterator|SchemaConcatIterator.
	// The yielded element type.
	Elem *SchemaType

	// Nullable: wraps any type to allow null.
	Nullable bool

	// Cog-specific annotations.
	// These carry through to x-cog-* keys in the OpenAPI output.
	CogArrayType    string // "iterator" for Iterator/ConcatIterator
	CogArrayDisplay string // "concatenate" for ConcatIterator
}

// SchemaTypeKind tags the active variant in SchemaType.
type SchemaTypeKind int

const (
	// SchemaPrimitive is a scalar type: bool, int, float, str, Path, File, Secret.
	SchemaPrimitive SchemaTypeKind = iota
	// SchemaAny is an opaque JSON value (unparameterized dict, Any, etc).
	SchemaAny
	// SchemaArray is a homogeneous list/array.
	SchemaArray
	// SchemaDict is a string-keyed dictionary with a typed value.
	SchemaDict
	// SchemaObject is a product type with named fields (BaseModel, TypedDict, dataclass).
	SchemaObject
	// SchemaIterator is a cog Iterator[T] — array with x-cog-array-type=iterator.
	SchemaIterator
	// SchemaConcatIterator is a cog ConcatenateIterator[str] — streaming text.
	SchemaConcatIterator
)

// SchemaField is a named field within a SchemaObject.
type SchemaField struct {
	Type     SchemaType
	Default  *DefaultValue
	Required bool
}

// JSONSchema converts a SchemaType to its JSON Schema representation.
// This is used for the "Output" component in the OpenAPI spec.
func (s SchemaType) JSONSchema() map[string]any {
	return s.jsonSchema(true)
}

func (s SchemaType) jsonSchema(topLevel bool) map[string]any {
	result := s.coreSchema()

	if topLevel {
		result["title"] = "Output"
	}

	if s.Nullable {
		result["nullable"] = true
	}

	return result
}

func (s SchemaType) coreSchema() map[string]any {
	switch s.Kind {
	case SchemaPrimitive:
		return s.Primitive.JSONType()

	case SchemaAny:
		return map[string]any{"type": "object"}

	case SchemaArray:
		items := map[string]any{"type": "object"}
		if s.Items != nil {
			items = s.Items.jsonSchema(false)
		}
		result := map[string]any{
			"type":  "array",
			"items": items,
		}
		return result

	case SchemaDict:
		result := map[string]any{"type": "object"}
		if s.ValueType != nil {
			result["additionalProperties"] = s.ValueType.jsonSchema(false)
		}
		return result

	case SchemaObject:
		if s.Fields == nil {
			return map[string]any{"type": "object"}
		}
		properties := make(map[string]any)
		var required []string
		s.Fields.Entries(func(name string, field SchemaField) {
			prop := field.Type.jsonSchema(false)
			prop["title"] = TitleCase(name)
			if !field.Required {
				prop["nullable"] = true
			}
			if field.Required && field.Default == nil {
				required = append(required, name)
			}
			properties[name] = prop
		})
		result := map[string]any{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			result["required"] = required
		}
		return result

	case SchemaIterator:
		items := map[string]any{"type": "object"}
		if s.Elem != nil {
			items = s.Elem.jsonSchema(false)
		}
		return map[string]any{
			"type":             "array",
			"items":            items,
			"x-cog-array-type": "iterator",
		}

	case SchemaConcatIterator:
		items := map[string]any{"type": "object"}
		if s.Elem != nil {
			items = s.Elem.jsonSchema(false)
		}
		return map[string]any{
			"type":                "array",
			"items":               items,
			"x-cog-array-type":    "iterator",
			"x-cog-array-display": "concatenate",
		}
	}

	return map[string]any{"type": "object"}
}

// ---------------------------------------------------------------------------
// Constructors — convenience functions for building SchemaType values.
// ---------------------------------------------------------------------------

// SchemaPrim creates a primitive SchemaType.
func SchemaPrim(p PrimitiveType) SchemaType {
	return SchemaType{Kind: SchemaPrimitive, Primitive: p}
}

// SchemaAnyType creates an opaque JSON object type.
func SchemaAnyType() SchemaType {
	return SchemaType{Kind: SchemaAny}
}

// SchemaArrayOf creates an array type with the given element type.
func SchemaArrayOf(elem SchemaType) SchemaType {
	return SchemaType{Kind: SchemaArray, Items: &elem}
}

// SchemaDictOf creates a dict type with string keys and the given value type.
func SchemaDictOf(value SchemaType) SchemaType {
	k := SchemaPrim(TypeString)
	return SchemaType{Kind: SchemaDict, KeyType: &k, ValueType: &value}
}

// SchemaIteratorOf creates an iterator type with the given element type.
func SchemaIteratorOf(elem SchemaType) SchemaType {
	return SchemaType{Kind: SchemaIterator, Elem: &elem}
}

// SchemaConcatIteratorOf creates a concatenate iterator type (always str).
func SchemaConcatIteratorOf() SchemaType {
	elem := SchemaPrim(TypeString)
	return SchemaType{Kind: SchemaConcatIterator, Elem: &elem}
}

// SchemaObjectOf creates an object type from an ordered map of fields.
func SchemaObjectOf(fields *OrderedMap[string, SchemaField]) SchemaType {
	return SchemaType{Kind: SchemaObject, Fields: fields}
}

// ---------------------------------------------------------------------------
// ResolveSchemaType — recursive output type resolver (replaces ResolveOutputType).
// ---------------------------------------------------------------------------

// ResolveSchemaType resolves a Python type annotation into a SchemaType.
// Unlike the legacy ResolveOutputType, this handles arbitrary nesting:
//
//	dict[str, list[dict[str, int]]]  →  SchemaDict{ValueType: SchemaArray{Items: SchemaDict{...}}}
//	list[dict[str, str]]             →  SchemaArray{Items: SchemaDict{ValueType: SchemaPrim(TypeString)}}
//
// It also resolves BaseModel subclasses and cog iterators.
func ResolveSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap) (SchemaType, error) {
	switch ann.Kind {
	case TypeAnnotSimple:
		return resolveSimpleSchemaType(ann, ctx, models)
	case TypeAnnotGeneric:
		return resolveGenericSchemaType(ann, ctx, models)
	case TypeAnnotUnion:
		return resolveUnionSchemaType(ann)
	}
	return SchemaType{}, errUnsupportedType("unknown type annotation")
}

func resolveSimpleSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap) (SchemaType, error) {
	// Check for BaseModel subclass
	if fields, ok := models.Get(ann.Name); ok {
		return resolveModelToSchemaType(fields, ctx, models)
	}

	// Unparameterized dict → opaque JSON object
	if ann.Name == "Any" || ann.Name == "dict" || ann.Name == "Dict" {
		return SchemaAnyType(), nil
	}

	// Unparameterized list → array of opaque objects
	if ann.Name == "list" || ann.Name == "List" {
		return SchemaArrayOf(SchemaAnyType()), nil
	}

	prim, ok := PrimitiveFromName(ann.Name)
	if !ok {
		// Check if this name was imported from an external package
		if entry, imported := ctx.Names.Get(ann.Name); imported {
			return SchemaType{}, errUnresolvableImportedType(ann.Name, entry.Module)
		}
		return SchemaType{}, errUnresolvableType(ann.Name)
	}
	return SchemaPrim(prim), nil
}

func resolveGenericSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap) (SchemaType, error) {
	outer := ann.Name

	// dict[K, V] — recursively resolve value type
	if outer == "dict" || outer == "Dict" {
		if len(ann.Args) == 2 {
			valType, err := ResolveSchemaType(ann.Args[1], ctx, models)
			if err != nil {
				// Fall back to opaque dict on unresolvable value type
				return SchemaAnyType(), nil
			}
			return SchemaDictOf(valType), nil
		}
		// dict with wrong arity → opaque
		return SchemaAnyType(), nil
	}

	// Optional[X] → resolve inner, set nullable
	if outer == "Optional" {
		if len(ann.Args) != 1 {
			return SchemaType{}, errUnsupportedType("Optional expects exactly 1 type argument")
		}
		// Optional is not allowed as an output type
		return SchemaType{}, errOptionalOutput()
	}

	// Union[X, Y] → delegate
	if outer == "Union" {
		return resolveUnionSchemaType(TypeAnnotation{Kind: TypeAnnotUnion, Args: ann.Args})
	}

	// list[X] / List[X]
	if outer == "List" || outer == "list" {
		if len(ann.Args) != 1 {
			return SchemaType{}, errUnsupportedType("list expects exactly 1 type argument")
		}
		elemType, err := ResolveSchemaType(ann.Args[0], ctx, models)
		if err != nil {
			return SchemaType{}, err
		}
		return SchemaArrayOf(elemType), nil
	}

	// Cog iterators — single type arg, must be simple (no nested generics in iterators)
	if outer == "Iterator" || outer == "AsyncIterator" {
		if len(ann.Args) != 1 {
			return SchemaType{}, errUnsupportedType("Iterator expects exactly 1 type argument")
		}
		elemType, err := ResolveSchemaType(ann.Args[0], ctx, models)
		if err != nil {
			return SchemaType{}, err
		}
		return SchemaIteratorOf(elemType), nil
	}

	if outer == "ConcatenateIterator" || outer == "AsyncConcatenateIterator" {
		if len(ann.Args) != 1 {
			return SchemaType{}, errUnsupportedType("ConcatenateIterator expects exactly 1 type argument")
		}
		inner := ann.Args[0]
		if inner.Kind != TypeAnnotSimple {
			return SchemaType{}, errUnsupportedType("ConcatenateIterator element type must be a simple type")
		}
		prim, ok := PrimitiveFromName(inner.Name)
		if !ok || prim != TypeString {
			return SchemaType{}, errConcatIteratorNotStr(inner.Name)
		}
		return SchemaConcatIteratorOf(), nil
	}

	return SchemaType{}, errUnsupportedType(fmt.Sprintf("%s[...] is not a supported output type", outer))
}

func resolveUnionSchemaType(ann TypeAnnotation) (SchemaType, error) {
	for _, m := range ann.Args {
		if m.Kind == TypeAnnotSimple && m.Name == "None" {
			return SchemaType{}, errOptionalOutput()
		}
	}
	return SchemaType{}, errUnsupportedType("union types are not supported as output")
}

func resolveModelToSchemaType(modelFields []ModelField, ctx *ImportContext, models ModelClassMap) (SchemaType, error) {
	fields := NewOrderedMap[string, SchemaField]()
	for _, f := range modelFields {
		ft, err := ResolveFieldType(f.Type, ctx)
		if err != nil {
			return SchemaType{}, err
		}
		st := fieldTypeToSchemaType(ft)
		required := ft.Repetition != Optional
		fields.Set(f.Name, SchemaField{
			Type:     st,
			Default:  f.Default,
			Required: required,
		})
	}
	return SchemaObjectOf(fields), nil
}

func fieldTypeToSchemaType(ft FieldType) SchemaType {
	base := SchemaPrim(ft.Primitive)
	if ft.Repetition == Repeated {
		return SchemaArrayOf(base)
	}
	if ft.Repetition == Optional {
		base.Nullable = true
	}
	return base
}
