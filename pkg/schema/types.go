package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Mode selects whether to extract predict or train signatures.
type Mode int

const (
	ModePredict Mode = iota
	ModeTrain
)

// PrimitiveType maps Python types to JSON Schema types.
type PrimitiveType int

const (
	TypeBool PrimitiveType = iota
	TypeFloat
	TypeInteger
	TypeString
	TypePath   // cog.Path — {"type":"string","format":"uri"}
	TypeFile   // cog.File (deprecated) — same wire format as Path
	TypeSecret // cog.Secret — write-only, masked
	TypeAny    // typing.Any or unresolved
)

// JSONType returns the JSON Schema fragment for this primitive.
func (p PrimitiveType) JSONType() map[string]any {
	switch p {
	case TypeBool:
		return map[string]any{"type": "boolean"}
	case TypeFloat:
		return map[string]any{"type": "number"}
	case TypeInteger:
		return map[string]any{"type": "integer"}
	case TypeString:
		return map[string]any{"type": "string"}
	case TypePath, TypeFile:
		return map[string]any{"type": "string", "format": "uri"}
	case TypeSecret:
		return map[string]any{"type": "string", "format": "password", "writeOnly": true, "x-cog-secret": true}
	case TypeAny:
		return map[string]any{"type": "object"}
	default:
		return map[string]any{"type": "object"}
	}
}

func (p PrimitiveType) String() string {
	names := [...]string{"bool", "float", "int", "str", "Path", "File", "Secret", "Any"}
	if int(p) < len(names) {
		return names[p]
	}
	return "unknown"
}

// PrimitiveFromName resolves a simple type name to a PrimitiveType.
func PrimitiveFromName(name string) (PrimitiveType, bool) {
	switch name {
	case "bool":
		return TypeBool, true
	case "float":
		return TypeFloat, true
	case "int":
		return TypeInteger, true
	case "str":
		return TypeString, true
	case "Path":
		return TypePath, true
	case "File":
		return TypeFile, true
	case "Secret":
		return TypeSecret, true
	case "Any":
		return TypeAny, true
	default:
		return 0, false
	}
}

// Repetition describes cardinality of a field.
type Repetition int

const (
	Required Repetition = iota
	Optional
	Repeated // list[X]
)

// FieldType combines a primitive type with its cardinality.
type FieldType struct {
	Primitive  PrimitiveType
	Repetition Repetition
}

// JSONType returns the JSON Schema fragment for this field type.
func (ft FieldType) JSONType() map[string]any {
	if ft.Repetition == Repeated {
		return map[string]any{
			"type":  "array",
			"items": ft.Primitive.JSONType(),
		}
	}
	return ft.Primitive.JSONType()
}

// DefaultValue represents a statically-parsed Python literal.
type DefaultValue struct {
	Kind     DefaultKind
	Bool     bool
	Int      int64
	Float    float64
	Str      string
	List     []DefaultValue
	DictKeys []DefaultValue // parallel with DictVals
	DictVals []DefaultValue
}

// DefaultKind tags the active field in DefaultValue.
type DefaultKind int

const (
	DefaultNone DefaultKind = iota
	DefaultBool
	DefaultInt
	DefaultFloat
	DefaultString
	DefaultList
	DefaultDict
	DefaultSet
)

// ToJSON converts a DefaultValue to its JSON representation.
func (d DefaultValue) ToJSON() any {
	switch d.Kind {
	case DefaultNone:
		return nil
	case DefaultBool:
		return d.Bool
	case DefaultInt:
		return d.Int
	case DefaultFloat:
		return d.Float
	case DefaultString:
		return d.Str
	case DefaultList, DefaultSet:
		items := make([]any, len(d.List))
		for i, v := range d.List {
			items[i] = v.ToJSON()
		}
		return items
	case DefaultDict:
		m := make(map[string]any, len(d.DictKeys))
		for i := range d.DictKeys {
			key := fmt.Sprintf("%v", d.DictKeys[i].ToJSON())
			if d.DictKeys[i].Kind == DefaultString {
				key = d.DictKeys[i].Str
			}
			m[key] = d.DictVals[i].ToJSON()
		}
		return m
	default:
		return nil
	}
}

// MarshalJSON implements json.Marshaler for DefaultValue.
func (d DefaultValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.ToJSON())
}

// InputField represents one parameter of predict/train.
type InputField struct {
	Name        string
	Order       int
	FieldType   FieldType
	Default     *DefaultValue
	Description *string
	GE          *float64
	LE          *float64
	MinLength   *uint64
	MaxLength   *uint64
	Regex       *string
	Choices     []DefaultValue
	Deprecated  *bool
}

// IsRequired returns true if this field is required in the schema.
func (f *InputField) IsRequired() bool {
	return f.Default == nil && (f.FieldType.Repetition == Required || f.FieldType.Repetition == Repeated)
}

// OutputKind describes the shape of the output type.
type OutputKind int

const (
	OutputSingle OutputKind = iota
	OutputList
	OutputIterator
	OutputConcatenateIterator
	OutputObject
)

// ObjectField represents a field in a BaseModel output type.
type ObjectField struct {
	FieldType FieldType
	Default   *DefaultValue
}

// OutputType describes the return type of predict/train.
type OutputType struct {
	Kind      OutputKind
	Primitive *PrimitiveType // for Single/List/Iterator/ConcatIterator
	Fields    *OrderedMap[string, ObjectField]
}

// JSONType returns the JSON Schema fragment for this output type.
func (o OutputType) JSONType() map[string]any {
	switch o.Kind {
	case OutputSingle:
		v := o.elementJSONType()
		v["title"] = "Output"
		return v
	case OutputList:
		return map[string]any{
			"title": "Output",
			"type":  "array",
			"items": o.elementJSONType(),
		}
	case OutputIterator:
		return map[string]any{
			"title":            "Output",
			"type":             "array",
			"items":            o.elementJSONType(),
			"x-cog-array-type": "iterator",
		}
	case OutputConcatenateIterator:
		return map[string]any{
			"title":               "Output",
			"type":                "array",
			"items":               o.elementJSONType(),
			"x-cog-array-type":    "iterator",
			"x-cog-array-display": "concatenate",
		}
	case OutputObject:
		if o.Fields == nil {
			return map[string]any{"title": "Output", "type": "object"}
		}
		properties := make(map[string]any)
		var required []string
		o.Fields.Entries(func(name string, field ObjectField) {
			prop := field.FieldType.JSONType()
			prop["title"] = TitleCase(name)
			if field.FieldType.Repetition == Optional {
				prop["nullable"] = true
			}
			if field.Default == nil && field.FieldType.Repetition != Optional {
				required = append(required, name)
			}
			properties[name] = prop
		})
		schema := map[string]any{
			"title":      "Output",
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	default:
		return map[string]any{"title": "Output", "type": "object"}
	}
}

func (o OutputType) elementJSONType() map[string]any {
	if o.Primitive != nil {
		return o.Primitive.JSONType()
	}
	return map[string]any{"type": "object"}
}

// PredictorInfo is the top-level extraction result.
type PredictorInfo struct {
	Inputs *OrderedMap[string, InputField]
	Output OutputType
	Mode   Mode
}

// TypeAnnotation is a parsed Python type annotation (intermediate, before resolution).
type TypeAnnotation struct {
	Kind TypeAnnotationKind
	Name string           // for Simple
	Args []TypeAnnotation // for Generic (outer=Name, args=Args) or Union (members=Args)
}

// TypeAnnotationKind tags the variant.
type TypeAnnotationKind int

const (
	TypeAnnotSimple TypeAnnotationKind = iota
	TypeAnnotGeneric
	TypeAnnotUnion
)

// ImportContext tracks what names are imported from which modules.
type ImportContext struct {
	// Names maps local name → (module, original_name)
	Names *OrderedMap[string, ImportEntry]
}

// ImportEntry records where a name was imported from.
type ImportEntry struct {
	Module   string
	Original string
}

// NewImportContext creates an empty ImportContext.
func NewImportContext() *ImportContext {
	return &ImportContext{Names: NewOrderedMap[string, ImportEntry]()}
}

// IsCogType returns true if name was imported from the "cog" module.
func (ctx *ImportContext) IsCogType(name string) bool {
	if e, ok := ctx.Names.Get(name); ok {
		return e.Module == "cog"
	}
	return false
}

// IsTypingType returns true if name was imported from "typing" or "typing_extensions".
func (ctx *ImportContext) IsTypingType(name string) bool {
	if e, ok := ctx.Names.Get(name); ok {
		return e.Module == "typing" || e.Module == "typing_extensions"
	}
	return false
}

// IsBaseModel returns true if name resolves to cog.BaseModel or pydantic.BaseModel.
func (ctx *ImportContext) IsBaseModel(name string) bool {
	if e, ok := ctx.Names.Get(name); ok {
		return (e.Module == "cog" || e.Module == "pydantic" || e.Module == "pydantic.v1") && e.Original == "BaseModel"
	}
	return false
}

// IsBasePredictor returns true if name resolves to cog.BasePredictor.
func (ctx *ImportContext) IsBasePredictor(name string) bool {
	if e, ok := ctx.Names.Get(name); ok {
		return e.Module == "cog" && e.Original == "BasePredictor"
	}
	return false
}

// ResolveFieldType resolves a TypeAnnotation into a FieldType.
func ResolveFieldType(ann TypeAnnotation, ctx *ImportContext) (FieldType, error) {
	switch ann.Kind {
	case TypeAnnotSimple:
		prim, ok := PrimitiveFromName(ann.Name)
		if !ok {
			return FieldType{}, errUnsupportedType(ann.Name)
		}
		return FieldType{Primitive: prim, Repetition: Required}, nil

	case TypeAnnotGeneric:
		outer := ann.Name
		if outer == "Optional" {
			if len(ann.Args) != 1 {
				return FieldType{}, errUnsupportedType(fmt.Sprintf("Optional expects exactly 1 type argument, got %d", len(ann.Args)))
			}
			inner, err := ResolveFieldType(ann.Args[0], ctx)
			if err != nil {
				return FieldType{}, err
			}
			return FieldType{Primitive: inner.Primitive, Repetition: Optional}, nil
		}
		if outer == "Union" {
			// typing.Union[X, Y] → treat as union type
			return ResolveFieldType(TypeAnnotation{Kind: TypeAnnotUnion, Args: ann.Args}, ctx)
		}
		if outer == "List" || outer == "list" {
			if len(ann.Args) != 1 {
				return FieldType{}, errUnsupportedType(fmt.Sprintf("List expects exactly 1 type argument, got %d", len(ann.Args)))
			}
			inner, err := ResolveFieldType(ann.Args[0], ctx)
			if err != nil {
				return FieldType{}, err
			}
			if inner.Repetition != Required {
				return FieldType{}, errUnsupportedType("nested generics like List[Optional[X]] are not supported")
			}
			return FieldType{Primitive: inner.Primitive, Repetition: Repeated}, nil
		}
		return FieldType{}, errUnsupportedType(fmt.Sprintf("%s[...] is not a supported input type", outer))

	case TypeAnnotUnion:
		if len(ann.Args) == 2 {
			hasNone := false
			var nonNone *TypeAnnotation
			for i := range ann.Args {
				if ann.Args[i].Kind == TypeAnnotSimple && ann.Args[i].Name == "None" {
					hasNone = true
				} else {
					a := ann.Args[i]
					nonNone = &a
				}
			}
			if hasNone && nonNone != nil {
				inner, err := ResolveFieldType(*nonNone, ctx)
				if err != nil {
					return FieldType{}, err
				}
				return FieldType{Primitive: inner.Primitive, Repetition: Optional}, nil
			}
		}
		return FieldType{}, errUnsupportedType("union types other than X | None are not supported")
	}
	return FieldType{}, errUnsupportedType("unknown type annotation")
}

// ModelClassMap maps class names to their fields.
type ModelClassMap = *OrderedMap[string, []ModelField]

// ModelField is a field extracted from a BaseModel subclass.
type ModelField struct {
	Name    string
	Type    TypeAnnotation
	Default *DefaultValue
}

// ResolveOutputType resolves an output type annotation.
func ResolveOutputType(ann TypeAnnotation, ctx *ImportContext, models ModelClassMap) (OutputType, error) {
	switch ann.Kind {
	case TypeAnnotSimple:
		// Check for BaseModel subclass
		if fields, ok := models.Get(ann.Name); ok {
			objFields := NewOrderedMap[string, ObjectField]()
			for _, f := range fields {
				ft, err := ResolveFieldType(f.Type, ctx)
				if err != nil {
					return OutputType{}, err
				}
				objFields.Set(f.Name, ObjectField{FieldType: ft, Default: f.Default})
			}
			return OutputType{Kind: OutputObject, Fields: objFields}, nil
		}
		if ann.Name == "Any" {
			p := TypeAny
			return OutputType{Kind: OutputSingle, Primitive: &p}, nil
		}
		prim, ok := PrimitiveFromName(ann.Name)
		if !ok {
			return OutputType{}, errUnsupportedType(ann.Name)
		}
		return OutputType{Kind: OutputSingle, Primitive: &prim}, nil

	case TypeAnnotGeneric:
		if len(ann.Args) != 1 {
			return OutputType{}, errUnsupportedType(fmt.Sprintf("%s expects exactly 1 type argument", ann.Name))
		}
		inner := ann.Args[0]
		if inner.Kind != TypeAnnotSimple {
			return OutputType{}, errUnsupportedType("nested generics in output type are not supported")
		}
		innerPrim, ok := PrimitiveFromName(inner.Name)
		if !ok {
			return OutputType{}, errUnsupportedType(inner.Name)
		}
		switch ann.Name {
		case "Iterator", "AsyncIterator":
			return OutputType{Kind: OutputIterator, Primitive: &innerPrim}, nil
		case "ConcatenateIterator", "AsyncConcatenateIterator":
			if innerPrim != TypeString {
				return OutputType{}, errConcatIteratorNotStr(innerPrim.String())
			}
			return OutputType{Kind: OutputConcatenateIterator, Primitive: &innerPrim}, nil
		case "List", "list":
			return OutputType{Kind: OutputList, Primitive: &innerPrim}, nil
		case "Optional":
			return OutputType{}, errOptionalOutput()
		default:
			return OutputType{}, errUnsupportedType(fmt.Sprintf("%s[...] is not a supported output type", ann.Name))
		}

	case TypeAnnotUnion:
		for _, m := range ann.Args {
			if m.Kind == TypeAnnotSimple && m.Name == "None" {
				return OutputType{}, errOptionalOutput()
			}
		}
		return OutputType{}, errUnsupportedType("union types are not supported as output")
	}
	return OutputType{}, errUnsupportedType("unknown type annotation")
}

// TitleCase converts snake_case to Title Case.
func TitleCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// TitleCaseSingle title-cases a single word (first letter uppercase).
func TitleCaseSingle(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// OrderedMap is a simple insertion-ordered map.
type OrderedMap[K comparable, V any] struct {
	keys   []K
	values map[K]V
}

// NewOrderedMap creates a new empty OrderedMap.
func NewOrderedMap[K comparable, V any]() *OrderedMap[K, V] {
	return &OrderedMap[K, V]{values: make(map[K]V)}
}

// Set inserts or updates a key-value pair.
func (m *OrderedMap[K, V]) Set(key K, value V) {
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Get returns the value for a key and whether it exists.
func (m *OrderedMap[K, V]) Get(key K) (V, bool) {
	v, ok := m.values[key]
	return v, ok
}

// Keys returns keys in insertion order.
func (m *OrderedMap[K, V]) Keys() []K {
	return m.keys
}

// Len returns the number of entries.
func (m *OrderedMap[K, V]) Len() int {
	return len(m.keys)
}

// Entries iterates over key-value pairs in insertion order.
func (m *OrderedMap[K, V]) Entries(fn func(key K, value V)) {
	for _, k := range m.keys {
		fn(k, m.values[k])
	}
}
