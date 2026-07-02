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
	Repeated         // list[X]
	OptionalRepeated // list[X] | None
)

// FieldType combines a primitive type with its cardinality.
type FieldType struct {
	Primitive  PrimitiveType
	Repetition Repetition
}

// InputTypeKind tags the recursive input type representation.
type InputTypeKind int

const (
	InputKindPrimitive InputTypeKind = iota
	InputKindAny
	InputKindArray
	InputKindUnion
)

// InputType represents JSON-native input types, including unions.
type InputType struct {
	Kind      InputTypeKind
	Primitive PrimitiveType
	Elem      *InputType
	Variants  []InputType
	Nullable  bool
}

// InputPrimitive creates a primitive input type.
func InputPrimitive(primitive PrimitiveType) InputType {
	if primitive == TypeAny {
		return InputAnyType()
	}
	return InputType{Kind: InputKindPrimitive, Primitive: primitive}
}

// InputAnyType creates an opaque JSON input type.
func InputAnyType() InputType {
	return InputType{Kind: InputKindAny, Primitive: TypeAny}
}

// InputArrayOf creates an array input type.
func InputArrayOf(elem InputType) InputType {
	return InputType{Kind: InputKindArray, Elem: &elem}
}

// InputUnionOf creates a union input type.
func InputUnionOf(variants ...InputType) InputType {
	return InputType{Kind: InputKindUnion, Variants: variants}
}

// JSONType returns the JSON Schema fragment for this field type.
func (ft FieldType) JSONType() map[string]any {
	if ft.Repetition == Repeated || ft.Repetition == OptionalRepeated {
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
	InputType   *InputType
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

// ValidateInputField checks combinations unsupported by the static input model.
func ValidateInputField(field InputField) error {
	if field.InputType != nil && field.InputType.Kind == InputKindUnion {
		if len(field.Choices) > 0 || field.GE != nil || field.LE != nil || field.MinLength != nil || field.MaxLength != nil || field.Regex != nil {
			return errUnsupportedType("constraints and choices are not supported on union inputs")
		}
	}
	return nil
}

// PredictorInfo is the top-level extraction result.
type PredictorInfo struct {
	Inputs            *OrderedMap[string, InputField]
	Output            SchemaType
	Mode              Mode
	SupportsStreaming bool
	ConcurrencyMax    *int
	IsAsync           bool
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

// IsTypedDict returns true if name resolves to typing.TypedDict or typing_extensions.TypedDict.
func (ctx *ImportContext) IsTypedDict(name string) bool {
	if e, ok := ctx.Names.Get(name); ok {
		return (e.Module == "typing" || e.Module == "typing_extensions") && e.Original == "TypedDict"
	}
	return false
}

// IsOpaque returns true if name resolves specifically to cog.Opaque.
func (ctx *ImportContext) IsOpaque(name string) bool {
	if resolved, entry, ok := ctx.ResolveQualifiedName(name); ok {
		return resolved == "Opaque" && entry.Module == "cog"
	}
	if e, ok := ctx.Names.Get(name); ok {
		return e.Module == "cog" && e.Original == "Opaque"
	}
	return false
}

func (ctx *ImportContext) isAnnotated(name string) bool {
	if resolved, entry, ok := ctx.ResolveQualifiedName(name); ok {
		return resolved == "Annotated" && (entry.Module == "typing" || entry.Module == "typing_extensions") && entry.Original == entry.Module
	}
	if e, ok := ctx.Names.Get(name); ok {
		return (e.Module == "typing" || e.Module == "typing_extensions") && e.Original == "Annotated"
	}
	return false
}

// IsTypedDictFieldQualifier returns true if name resolves to Required or NotRequired.
func (ctx *ImportContext) IsTypedDictFieldQualifier(name string) bool {
	if e, ok := ctx.Names.Get(name); ok {
		return (e.Module == "typing" || e.Module == "typing_extensions") && (e.Original == "Required" || e.Original == "NotRequired")
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

// ResolveQualifiedName unwraps module-qualified names like alias.Type to Type
// when alias is known in the import context.
func (ctx *ImportContext) ResolveQualifiedName(name string) (string, ImportEntry, bool) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return name, ImportEntry{}, false
	}
	entry, ok := ctx.Names.Get(parts[0])
	if !ok {
		return parts[1], ImportEntry{}, true
	}
	return parts[1], entry, true
}

// ResolveFieldType resolves a TypeAnnotation into a FieldType.
func ResolveFieldType(ann TypeAnnotation, ctx *ImportContext, typedDicts map[string]bool) (FieldType, error) {
	if inner, ok := unwrapOpaqueAnnotated(ann, ctx); ok {
		return opaqueFieldType(inner, ctx), nil
	}

	switch ann.Kind {
	case TypeAnnotSimple:
		name := ann.Name
		if typedDicts[name] {
			return FieldType{Primitive: TypeAny, Repetition: Required}, nil
		}
		qualifiedEntry := ImportEntry{}
		if resolved, entry, ok := ctx.ResolveQualifiedName(name); ok {
			name = resolved
			qualifiedEntry = entry
			if typedDicts[entry.Original+"."+name] {
				return FieldType{Primitive: TypeAny, Repetition: Required}, nil
			}
		}
		if typedDicts[name] {
			return FieldType{Primitive: TypeAny, Repetition: Required}, nil
		}
		// Bare dict / Dict → opaque JSON object (TypeAny)
		if name == "dict" || name == "Dict" {
			return FieldType{Primitive: TypeAny, Repetition: Required}, nil
		}
		prim, ok := PrimitiveFromName(name)
		if !ok {
			if qualifiedEntry.Module != "" {
				return FieldType{}, errUnresolvableImportedType(name, qualifiedEntry.Module)
			}
			if entry, imported := ctx.Names.Get(name); imported {
				return FieldType{}, errUnresolvableImportedType(name, entry.Module)
			}
			return FieldType{}, errUnsupportedType(name)
		}
		return FieldType{Primitive: prim, Repetition: Required}, nil

	case TypeAnnotGeneric:
		outer := ann.Name
		if resolved, _, ok := ctx.ResolveQualifiedName(outer); ok {
			outer = resolved
		}
		// dict[K, V] / Dict[K, V] → opaque JSON object (TypeAny).
		// Type parameters are intentionally discarded because FieldType is flat
		// (PrimitiveType + Repetition only). The output path uses the recursive
		// SchemaType model which can represent typed dicts (e.g. dict[str, int])
		// precisely; for inputs, all dicts are treated as opaque JSON objects.
		if outer == "dict" || outer == "Dict" {
			return FieldType{Primitive: TypeAny, Repetition: Required}, nil
		}
		if outer == "Optional" {
			if len(ann.Args) != 1 {
				return FieldType{}, errUnsupportedType(fmt.Sprintf("Optional expects exactly 1 type argument, got %d", len(ann.Args)))
			}
			inner, err := ResolveFieldType(ann.Args[0], ctx, typedDicts)
			if err != nil {
				return FieldType{}, err
			}
			rep := Optional
			if inner.Repetition == Repeated {
				rep = OptionalRepeated
			}
			return FieldType{Primitive: inner.Primitive, Repetition: rep}, nil
		}
		if outer == "Union" {
			// typing.Union[X, Y] → treat as union type
			return ResolveFieldType(TypeAnnotation{Kind: TypeAnnotUnion, Args: ann.Args}, ctx, typedDicts)
		}
		if ctx.isAnnotated(ann.Name) {
			if len(ann.Args) == 0 {
				return FieldType{}, errUnsupportedType("Annotated expects at least 1 type argument")
			}
			return ResolveFieldType(ann.Args[0], ctx, typedDicts)
		}
		if outer == "List" || outer == "list" {
			if len(ann.Args) != 1 {
				return FieldType{}, errUnsupportedType(fmt.Sprintf("List expects exactly 1 type argument, got %d", len(ann.Args)))
			}
			if opaqueInner, ok := unwrapOpaqueAnnotated(ann.Args[0], ctx); ok {
				inner := opaqueFieldType(opaqueInner, ctx)
				if inner.Repetition != Required {
					return FieldType{}, errUnsupportedType("nested generics like List[Optional[X]] are not supported")
				}
				return FieldType{Primitive: inner.Primitive, Repetition: Repeated}, nil
			}
			inner, err := ResolveFieldType(ann.Args[0], ctx, typedDicts)
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
		if inner, ok := UnwrapOptional(ann); ok {
			ft, err := ResolveFieldType(inner, ctx, typedDicts)
			if err != nil {
				return FieldType{}, err
			}
			rep := Optional
			if ft.Repetition == Repeated {
				rep = OptionalRepeated
			}
			return FieldType{Primitive: ft.Primitive, Repetition: rep}, nil
		}
		return FieldType{}, errUnsupportedType("union types other than X | None are not supported")
	}
	return FieldType{}, errUnsupportedType("unknown type annotation")
}

// ResolveInputType resolves a TypeAnnotation into the recursive input type model
// and the legacy FieldType compatibility layer.
func ResolveInputType(ann TypeAnnotation, ctx *ImportContext, typedDicts map[string]bool) (InputType, FieldType, error) {
	inputType, err := resolveInputType(ann, ctx, typedDicts)
	if err != nil {
		return InputType{}, FieldType{}, err
	}
	return inputType, fieldTypeFromInputType(inputType), nil
}

func resolveInputType(ann TypeAnnotation, ctx *ImportContext, typedDicts map[string]bool) (InputType, error) {
	if inner, ok := unwrapOpaqueAnnotated(ann, ctx); ok {
		return inputTypeFromFieldType(opaqueFieldType(inner, ctx)), nil
	}

	switch ann.Kind {
	case TypeAnnotSimple:
		name := ann.Name
		if typedDicts[name] {
			return InputAnyType(), nil
		}
		qualifiedEntry := ImportEntry{}
		if resolved, entry, ok := ctx.ResolveQualifiedName(name); ok {
			name = resolved
			qualifiedEntry = entry
			if typedDicts[entry.Original+"."+name] {
				return InputAnyType(), nil
			}
		}
		if typedDicts[name] {
			return InputAnyType(), nil
		}
		if name == "dict" || name == "Dict" {
			return InputAnyType(), nil
		}
		prim, ok := PrimitiveFromName(name)
		if !ok {
			if qualifiedEntry.Module != "" {
				return InputType{}, errUnresolvableImportedType(name, qualifiedEntry.Module)
			}
			if entry, imported := ctx.Names.Get(name); imported {
				return InputType{}, errUnresolvableImportedType(name, entry.Module)
			}
			return InputType{}, errUnsupportedType(name)
		}
		return InputPrimitive(prim), nil

	case TypeAnnotGeneric:
		outer := ann.Name
		if resolved, _, ok := ctx.ResolveQualifiedName(outer); ok {
			outer = resolved
		}
		if outer == "dict" || outer == "Dict" {
			return InputAnyType(), nil
		}
		if outer == "Optional" {
			if len(ann.Args) != 1 {
				return InputType{}, errUnsupportedType(fmt.Sprintf("Optional expects exactly 1 type argument, got %d", len(ann.Args)))
			}
			inner, err := resolveInputType(ann.Args[0], ctx, typedDicts)
			if err != nil {
				return InputType{}, err
			}
			inner.Nullable = true
			return inner, nil
		}
		if outer == "Union" {
			return resolveInputUnion(ann.Args, ctx, typedDicts)
		}
		if ctx.isAnnotated(ann.Name) {
			if len(ann.Args) == 0 {
				return InputType{}, errUnsupportedType("Annotated expects at least 1 type argument")
			}
			return resolveInputType(ann.Args[0], ctx, typedDicts)
		}
		if outer == "List" || outer == "list" {
			if len(ann.Args) != 1 {
				return InputType{}, errUnsupportedType(fmt.Sprintf("List expects exactly 1 type argument, got %d", len(ann.Args)))
			}
			if opaqueInner, ok := unwrapOpaqueAnnotated(ann.Args[0], ctx); ok {
				inner := inputTypeFromFieldType(opaqueFieldType(opaqueInner, ctx))
				if inner.Nullable || inner.Kind == InputKindArray || inner.Kind == InputKindUnion {
					return InputType{}, errUnsupportedType("nested generics like List[Optional[X]] are not supported")
				}
				return InputArrayOf(inner), nil
			}
			inner, err := resolveInputType(ann.Args[0], ctx, typedDicts)
			if err != nil {
				return InputType{}, err
			}
			if inner.Nullable || inner.Kind == InputKindArray || inner.Kind == InputKindUnion {
				return InputType{}, errUnsupportedType("nested generics like List[Optional[X]] are not supported")
			}
			return InputArrayOf(inner), nil
		}
		return InputType{}, errUnsupportedType(fmt.Sprintf("%s[...] is not a supported input type", outer))

	case TypeAnnotUnion:
		return resolveInputUnion(ann.Args, ctx, typedDicts)
	}
	return InputType{}, errUnsupportedType("unknown type annotation")
}

func resolveInputUnion(args []TypeAnnotation, ctx *ImportContext, typedDicts map[string]bool) (InputType, error) {
	variants := make([]InputType, 0, len(args))
	nullable := false
	for _, arg := range args {
		if arg.Kind == TypeAnnotSimple && arg.Name == "None" {
			nullable = true
			continue
		}
		if arg.Kind == TypeAnnotUnion || (arg.Kind == TypeAnnotGeneric && arg.Name == "Union") {
			return InputType{}, errUnsupportedType("nested union inputs are not supported")
		}
		variant, err := resolveInputType(arg, ctx, typedDicts)
		if err != nil {
			return InputType{}, err
		}
		if variant.Kind == InputKindUnion {
			return InputType{}, errUnsupportedType("nested union inputs are not supported")
		}
		variants = append(variants, variant)
	}

	if len(variants) == 0 {
		return InputType{}, errUnsupportedType("union inputs must include at least one non-None type")
	}
	if len(variants) == 1 {
		variant := variants[0]
		variant.Nullable = variant.Nullable || nullable
		return variant, nil
	}
	for _, variant := range variants {
		if err := validateUnionVariant(variant); err != nil {
			return InputType{}, err
		}
	}

	union := InputUnionOf(variants...)
	union.Nullable = nullable
	return union, nil
}

func validateUnionVariant(inputType InputType) error {
	if inputType.Nullable {
		return errUnsupportedType("nested nullable variants are not supported in union inputs")
	}
	switch inputType.Kind {
	case InputKindPrimitive:
		if inputType.Primitive == TypePath || inputType.Primitive == TypeFile || inputType.Primitive == TypeSecret {
			return errUnsupportedType(fmt.Sprintf("%s is not supported in union inputs", inputType.Primitive))
		}
	case InputKindArray:
		if inputType.Elem != nil {
			return validateUnionVariant(*inputType.Elem)
		}
	case InputKindUnion:
		return errUnsupportedType("nested union inputs are not supported")
	}
	return nil
}

func inputTypeFromFieldType(fieldType FieldType) InputType {
	var inputType InputType
	if fieldType.Primitive == TypeAny {
		inputType = InputAnyType()
	} else {
		inputType = InputPrimitive(fieldType.Primitive)
	}
	if fieldType.Repetition == Repeated || fieldType.Repetition == OptionalRepeated {
		inputType = InputArrayOf(inputType)
	}
	if fieldType.Repetition == Optional || fieldType.Repetition == OptionalRepeated {
		inputType.Nullable = true
	}
	return inputType
}

func fieldTypeFromInputType(inputType InputType) FieldType {
	repetition := Required
	if inputType.Nullable {
		repetition = Optional
	}
	switch inputType.Kind {
	case InputKindPrimitive:
		return FieldType{Primitive: inputType.Primitive, Repetition: repetition}
	case InputKindArray:
		arrayRepetition := Repeated
		if inputType.Nullable {
			arrayRepetition = OptionalRepeated
		}
		if inputType.Elem != nil && inputType.Elem.Kind == InputKindPrimitive {
			return FieldType{Primitive: inputType.Elem.Primitive, Repetition: arrayRepetition}
		}
		return FieldType{Primitive: TypeAny, Repetition: arrayRepetition}
	case InputKindAny, InputKindUnion:
		return FieldType{Primitive: TypeAny, Repetition: repetition}
	default:
		return FieldType{Primitive: TypeAny, Repetition: repetition}
	}
}

func unwrapOpaqueAnnotated(ann TypeAnnotation, ctx *ImportContext) (TypeAnnotation, bool) {
	if ann.Kind != TypeAnnotGeneric || !ctx.isAnnotated(ann.Name) || len(ann.Args) < 2 {
		return ann, false
	}
	for _, metadata := range ann.Args[1:] {
		if metadata.Kind == TypeAnnotSimple && ctx.IsOpaque(metadata.Name) {
			return ann.Args[0], true
		}
	}
	return ann, false
}

func opaqueFieldType(inner TypeAnnotation, ctx *ImportContext) FieldType {
	if unwrapped, ok := unwrapOpaqueAnnotated(inner, ctx); ok {
		return opaqueFieldType(unwrapped, ctx)
	}

	if inner.Kind == TypeAnnotSimple {
		if name, ok := opaqueContainerName(inner.Name, ctx); ok && (name == "List" || name == "list") {
			return FieldType{Primitive: TypeAny, Repetition: Repeated}
		}
	}

	if inner.Kind == TypeAnnotGeneric {
		outer, outerIsContainer := opaqueContainerName(inner.Name, ctx)
		if optionalInner, ok := unwrapOpaqueOptional(inner, ctx); ok {
			fieldType := opaqueFieldType(optionalInner, ctx)
			repetition := Optional
			if fieldType.Repetition == Repeated {
				repetition = OptionalRepeated
			}
			return FieldType{Primitive: TypeAny, Repetition: repetition}
		}
		if !outerIsContainer {
			return FieldType{Primitive: TypeAny, Repetition: Required}
		}
		switch outer {
		case "List", "list":
			return FieldType{Primitive: TypeAny, Repetition: Repeated}
		case "Optional":
			if len(inner.Args) == 1 {
				fieldType := opaqueFieldType(inner.Args[0], ctx)
				repetition := Optional
				if fieldType.Repetition == Repeated {
					repetition = OptionalRepeated
				}
				return FieldType{Primitive: TypeAny, Repetition: repetition}
			}
		}
	}

	if inner.Kind == TypeAnnotUnion {
		if optionalInner, ok := UnwrapOptional(inner); ok {
			fieldType := opaqueFieldType(optionalInner, ctx)
			repetition := Optional
			if fieldType.Repetition == Repeated {
				repetition = OptionalRepeated
			}
			return FieldType{Primitive: TypeAny, Repetition: repetition}
		}
	}

	return FieldType{Primitive: TypeAny, Repetition: Required}
}

func opaqueContainerName(name string, ctx *ImportContext) (string, bool) {
	if resolved, entry, ok := ctx.ResolveQualifiedName(name); ok {
		if isTypingModule(entry.Module) && entry.Original == entry.Module && isTypingContainer(resolved) {
			return resolved, true
		}
		return "", false
	}
	if entry, ok := ctx.Names.Get(name); ok {
		if entry.Module == "builtins" && entry.Original == "list" {
			return "list", true
		}
		if isTypingModule(entry.Module) && isTypingContainer(entry.Original) {
			return entry.Original, true
		}
		return "", false
	}
	if name == "list" {
		return "list", true
	}
	return "", false
}

func isTypingModule(module string) bool {
	return module == "typing" || module == "typing_extensions"
}

func isTypingContainer(name string) bool {
	return name == "List" || name == "Optional" || name == "Union"
}

func unwrapOpaqueOptional(ann TypeAnnotation, ctx *ImportContext) (TypeAnnotation, bool) {
	if ann.Kind == TypeAnnotGeneric {
		name, ok := opaqueContainerName(ann.Name, ctx)
		if !ok {
			return ann, false
		}
		if name == "Optional" && len(ann.Args) == 1 {
			return ann.Args[0], true
		}
		if name == "Union" && len(ann.Args) == 2 {
			for i := range ann.Args {
				if ann.Args[i].Kind == TypeAnnotSimple && ann.Args[i].Name == "None" {
					return ann.Args[1-i], true
				}
			}
		}
	}
	return UnwrapOptional(ann)
}

// UnwrapOptional checks if a type annotation represents Optional[X] or Union[X, None].
// If so, it returns the inner type and true. Otherwise it returns the original and false.
func UnwrapOptional(ann TypeAnnotation) (TypeAnnotation, bool) {
	// Optional[X]
	if ann.Kind == TypeAnnotGeneric && ann.Name == "Optional" && len(ann.Args) == 1 {
		return ann.Args[0], true
	}
	// Union[X, None] or X | None
	args := ann.Args
	if (ann.Kind == TypeAnnotGeneric && ann.Name == "Union" || ann.Kind == TypeAnnotUnion) && len(args) == 2 {
		for i := range args {
			if args[i].Kind == TypeAnnotSimple && args[i].Name == "None" {
				return args[1-i], true
			}
		}
	}
	return ann, false
}

// ModelClassMap maps class names to their fields.
type ModelClassMap = *OrderedMap[string, []ModelField]

// ModelField is a field extracted from a BaseModel subclass.
type ModelField struct {
	Name        string
	Type        TypeAnnotation
	Default     *DefaultValue
	KeyRequired *bool
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
