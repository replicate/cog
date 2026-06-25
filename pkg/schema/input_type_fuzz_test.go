package schema

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

// FuzzResolveInputType builds arbitrary TypeAnnotation trees from fuzz input
// and verifies that ResolveInputType never panics. When resolution succeeds,
// it feeds the resulting InputType through OpenAPI generation and validates
// the emitted document with the same kin-openapi validator used at build time
// (writeAndValidateSchema). This is the key oracle: a union input type that
// resolves cleanly but emits an OpenAPI document the build-time validator
// rejects (e.g. an unsupported `type: null` branch) is a real bug, not just a
// panic.
func FuzzResolveInputType(f *testing.F) {
	// Seed corpus — union and JSON-native input shapes, plus tricky cases.
	seeds := []TypeAnnotation{
		{Kind: TypeAnnotSimple, Name: "str"},
		{Kind: TypeAnnotSimple, Name: "int"},
		{Kind: TypeAnnotSimple, Name: "float"},
		{Kind: TypeAnnotSimple, Name: "bool"},
		{Kind: TypeAnnotSimple, Name: "dict"},
		{Kind: TypeAnnotSimple, Name: "Any"},
		// str | float
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
			{Kind: TypeAnnotSimple, Name: "float"},
		}},
		// str | float | None
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
			{Kind: TypeAnnotSimple, Name: "float"},
			{Kind: TypeAnnotSimple, Name: "None"},
		}},
		// str | None (single-variant collapse to nullable)
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
			{Kind: TypeAnnotSimple, Name: "None"},
		}},
		// int | float
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "int"},
			{Kind: TypeAnnotSimple, Name: "float"},
		}},
		// list[int] | list[float]
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotGeneric, Name: "list", Args: []TypeAnnotation{
				{Kind: TypeAnnotSimple, Name: "int"},
			}},
			{Kind: TypeAnnotGeneric, Name: "list", Args: []TypeAnnotation{
				{Kind: TypeAnnotSimple, Name: "float"},
			}},
		}},
		// dict | list[dict]
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "dict"},
			{Kind: TypeAnnotGeneric, Name: "list", Args: []TypeAnnotation{
				{Kind: TypeAnnotSimple, Name: "dict"},
			}},
		}},
		// Unsupported member: Path | str (must be rejected, not panic)
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "Path"},
			{Kind: TypeAnnotSimple, Name: "str"},
		}},
		// Optional[str]
		{Kind: TypeAnnotGeneric, Name: "Optional", Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
		}},
	}
	for _, s := range seeds {
		f.Add(encodeAnnotation(s))
	}

	ctx := NewImportContext()
	typedDicts := map[string]bool{}

	f.Fuzz(func(t *testing.T, data []byte) {
		ann, _ := decodeAnnotation(data, 0, 0)

		// Must not panic regardless of input.
		it, ft, err := ResolveInputType(ann, ctx, typedDicts)
		if err != nil {
			return
		}

		// A resolved input type must build a field that validates and emits a
		// valid OpenAPI document.
		field := InputField{
			Name:      "value",
			Order:     0,
			FieldType: ft,
			InputType: &it,
		}
		if err := ValidateInputField(field); err != nil {
			return
		}

		inputs := NewOrderedMap[string, InputField]()
		inputs.Set("value", field)
		out, err := GenerateOpenAPISchema(&PredictorInfo{
			Inputs: inputs,
			Output: SchemaPrim(TypeString),
			Mode:   ModePredict,
		})
		if err != nil {
			return
		}

		// Oracle: the generated schema must be a valid OpenAPI document, the
		// same check writeAndValidateSchema performs at build time. A schema
		// that fails here would surface as a confusing build failure for users.
		assertValidOpenAPI(t, out)
	})
}

// FuzzInputTypeJSONSchema constructs arbitrary InputType trees directly (not via
// the annotation resolver) and ensures both the per-field JSON schema helper
// and full OpenAPI generation never panic and always emit a valid document.
// Building InputType directly reaches shapes the resolver may not produce,
// stressing inputTypeJSONSchema and buildInputSchema in isolation.
func FuzzInputTypeJSONSchema(f *testing.F) {
	f.Add([]byte{0, 3})                   // primitive string
	f.Add([]byte{1})                      // any
	f.Add([]byte{2, 0, 3})                // array of string
	f.Add([]byte{3, 2, 0, 3, 0, 1})       // union of string and float
	f.Add([]byte{3, 2, 0, 3, 0, 1, 0xff}) // nullable union of string and float

	f.Fuzz(func(t *testing.T, data []byte) {
		it, _ := decodeInputType(data, 0, 0)

		// Field with both a compat FieldType and the recursive InputType set,
		// mirroring how the parser populates InputField.
		field := InputField{
			Name:      "value",
			Order:     0,
			FieldType: FieldType{Primitive: TypeAny, Repetition: Required},
			InputType: &it,
		}

		inputs := NewOrderedMap[string, InputField]()
		inputs.Set("value", field)
		out, err := GenerateOpenAPISchema(&PredictorInfo{
			Inputs: inputs,
			Output: SchemaPrim(TypeString),
			Mode:   ModePredict,
		})
		if err != nil {
			return
		}
		assertValidOpenAPI(t, out)
	})
}

// assertValidOpenAPI loads and validates a generated OpenAPI document with the
// same kin-openapi validator used by writeAndValidateSchema at build time.
// A document that fails validation is a generation bug.
func assertValidOpenAPI(t *testing.T, schemaJSON []byte) {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromData(schemaJSON)
	require.NoError(t, err, "generated schema failed to load\n%s", string(schemaJSON))
	err = doc.Validate(context.Background())
	require.NoError(t, err, "generated schema is invalid\n%s", string(schemaJSON))
}

// decodeInputType builds an InputType tree from bytes, mirroring the encoding
// strategy of decodeSchemaType. The final byte of a primitive/union toggles
// nullability so the fuzzer reaches both nullable and non-nullable shapes.
func decodeInputType(data []byte, offset int, depth int) (InputType, int) {
	if depth > maxFuzzDepth || offset >= len(data) {
		return InputPrimitive(TypeString), offset
	}

	kind := InputTypeKind(data[offset] % 4)
	offset++

	switch kind {
	case InputKindPrimitive:
		prim := PrimitiveType(0)
		if offset < len(data) {
			prim = PrimitiveType(data[offset] % 9)
			offset++
		}
		it := InputPrimitive(prim)
		if offset < len(data) {
			if data[offset]%2 == 1 {
				it.Nullable = true
			}
			offset++
		}
		return it, offset

	case InputKindAny:
		it := InputAnyType()
		if offset < len(data) {
			if data[offset]%2 == 1 {
				it.Nullable = true
			}
			offset++
		}
		return it, offset

	case InputKindArray:
		elem, newOffset := decodeInputType(data, offset, depth+1)
		it := InputArrayOf(elem)
		if newOffset < len(data) {
			if data[newOffset]%2 == 1 {
				it.Nullable = true
			}
			newOffset++
		}
		return it, newOffset

	case InputKindUnion:
		numVariants := 0
		if offset < len(data) {
			numVariants = int(data[offset]) % 4 // cap at 3 variants
			offset++
		}
		variants := make([]InputType, 0, numVariants)
		for i := 0; i < numVariants && offset < len(data); i++ {
			v, newOffset := decodeInputType(data, offset, depth+1)
			variants = append(variants, v)
			offset = newOffset
		}
		it := InputUnionOf(variants...)
		if offset < len(data) {
			if data[offset]%2 == 1 {
				it.Nullable = true
			}
			offset++
		}
		return it, offset

	default:
		return InputPrimitive(TypeString), offset
	}
}
