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

	// Dict: for Kind=SchemaDict — the value type. Keys are always strings in JSON Schema.
	ValueType *SchemaType

	// Object: for Kind=SchemaObject — named fields with types and defaults.
	Fields *OrderedMap[string, SchemaField]

	// Iterator/ConcatIterator: for Kind=SchemaIterator|SchemaConcatIterator.
	// The yielded element type.
	Elem *SchemaType

	// Nullable: wraps any type to allow null.
	Nullable bool
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

// String returns the name of the SchemaTypeKind for diagnostic messages.
func (k SchemaTypeKind) String() string {
	switch k {
	case SchemaPrimitive:
		return "SchemaPrimitive"
	case SchemaAny:
		return "SchemaAny"
	case SchemaArray:
		return "SchemaArray"
	case SchemaDict:
		return "SchemaDict"
	case SchemaObject:
		return "SchemaObject"
	case SchemaIterator:
		return "SchemaIterator"
	case SchemaConcatIterator:
		return "SchemaConcatIterator"
	default:
		return fmt.Sprintf("SchemaTypeKind(%d)", int(k))
	}
}

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
		properties := newOrderedMapAny()
		var required []string
		s.Fields.Entries(func(name string, field SchemaField) {
			prop := field.Type.jsonSchema(false)
			prop["title"] = TitleCase(name)
			if field.Required && field.Default == nil {
				required = append(required, name)
			}
			properties.Set(name, prop)
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
	default:
		// All SchemaTypeKind values must be handled above. If this is reached,
		// it indicates a missing case (e.g. a new kind was added without updating
		// this switch). Panic to surface the bug immediately rather than silently
		// returning a wrong schema.
		panic(fmt.Sprintf("unhandled SchemaTypeKind: %s (%d)", s.Kind, int(s.Kind)))
	}
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
	return SchemaType{Kind: SchemaDict, ValueType: &value}
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
	return resolveSchemaType(ann, ctx, models, nil)
}

func resolveSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap, seen map[string]bool) (SchemaType, error) {
	switch ann.Kind {
	case TypeAnnotSimple:
		return resolveSimpleSchemaType(ann, ctx, models, seen)
	case TypeAnnotGeneric:
		return resolveGenericSchemaType(ann, ctx, models, seen)
	case TypeAnnotUnion:
		return resolveUnionSchemaType(ann)
	}
	return SchemaType{}, errUnsupportedType("unknown type annotation")
}

func resolveSimpleSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap, seen map[string]bool) (SchemaType, error) {
	name := ann.Name
	qualifiedEntry := ImportEntry{}
	if resolved, entry, ok := ctx.ResolveQualifiedName(name); ok {
		name = resolved
		qualifiedEntry = entry
	}

	// Check for BaseModel subclass
	if fields, ok := models.Get(name); ok {
		// Cycle detection: if we're already resolving this model, emit opaque object.
		if seen[name] {
			return SchemaAnyType(), nil
		}
		if seen == nil {
			seen = make(map[string]bool)
		}
		seen[name] = true
		defer delete(seen, name)
		return resolveModelToSchemaType(fields, ctx, models, seen)
	}

	// Unparameterized dict → opaque JSON object
	if name == "Any" || name == "dict" || name == "Dict" {
		return SchemaAnyType(), nil
	}

	// Unparameterized list → array of opaque objects
	if name == "list" || name == "List" {
		return SchemaArrayOf(SchemaAnyType()), nil
	}

	prim, ok := PrimitiveFromName(name)
	if !ok {
		// Check if this name was imported from an external package
		if qualifiedEntry.Module != "" {
			return SchemaType{}, errUnresolvableImportedType(name, qualifiedEntry.Module)
		}
		if entry, imported := ctx.Names.Get(name); imported {
			return SchemaType{}, errUnresolvableImportedType(name, entry.Module)
		}
		return SchemaType{}, errUnresolvableType(name)
	}
	return SchemaPrim(prim), nil
}

func resolveGenericSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap, seen map[string]bool) (SchemaType, error) {
	outer := ann.Name
	if resolved, _, ok := ctx.ResolveQualifiedName(outer); ok {
		outer = resolved
	}

	// dict[K, V] — recursively resolve value type
	if outer == "dict" || outer == "Dict" {
		if len(ann.Args) == 2 {
			valType, err := resolveSchemaType(ann.Args[1], ctx, models, seen)
			if err != nil {
				return SchemaType{}, fmt.Errorf("resolving dict value type: %w", err)
			}
			return SchemaDictOf(valType), nil
		}
		// Bare dict (no type args) → opaque
		if len(ann.Args) == 0 {
			return SchemaAnyType(), nil
		}
		return SchemaType{}, errUnsupportedType("dict expects 0 or 2 type arguments")
	}

	// Optional[X] → rejected as output type (nullable outputs not supported)
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
		elemType, err := resolveSchemaType(ann.Args[0], ctx, models, seen)
		if err != nil {
			return SchemaType{}, err
		}
		return SchemaArrayOf(elemType), nil
	}

	// Cog iterators — single type arg, recursively resolved (supports nested types)
	if outer == "Iterator" || outer == "AsyncIterator" {
		if len(ann.Args) != 1 {
			return SchemaType{}, errUnsupportedType("Iterator expects exactly 1 type argument")
		}
		elemType, err := resolveSchemaType(ann.Args[0], ctx, models, seen)
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
	if _, ok := UnwrapOptional(ann); ok {
		return SchemaType{}, errOptionalOutput()
	}
	return SchemaType{}, errUnsupportedType("union types are not supported as output")
}

// resolveModelToSchemaType converts a BaseModel's fields into a SchemaObject.
// Fields are resolved via resolveFieldSchemaType which supports the full recursive
// SchemaType system (dict[str, list[int]], nested BaseModels, etc.) plus Optional
// wrapping (which is valid for fields but not for top-level output types).
func resolveModelToSchemaType(modelFields []ModelField, ctx *ImportContext, models ModelClassMap, seen map[string]bool) (SchemaType, error) {
	fields := NewOrderedMap[string, SchemaField]()
	for _, f := range modelFields {
		st, required, err := resolveFieldSchemaType(f.Type, ctx, models, seen)
		if err != nil {
			return SchemaType{}, fmt.Errorf("field %q: %w", f.Name, err)
		}
		if f.KeyRequired != nil {
			required = *f.KeyRequired
		}
		if f.Default != nil {
			required = false
		}
		fields.Set(f.Name, SchemaField{
			Type:     st,
			Default:  f.Default,
			Required: required,
		})
	}
	return SchemaObjectOf(fields), nil
}

// resolveFieldSchemaType resolves a type annotation for a model field.
// Unlike ResolveSchemaType (which rejects Optional as a top-level output),
// this allows Optional[X] and Union[X, None] for fields, setting Nullable.
func resolveFieldSchemaType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap, seen map[string]bool) (SchemaType, bool, error) {
	if inner, ok := UnwrapOptional(ann); ok {
		st, err := resolveSchemaType(inner, ctx, models, seen)
		if err != nil {
			return SchemaType{}, false, err
		}
		st.Nullable = true
		return st, false, nil
	}

	st, err := resolveSchemaType(ann, ctx, models, seen)
	if err != nil {
		return SchemaType{}, false, err
	}
	return st, true, nil
}
