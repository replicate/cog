package schema

import (
	"testing"
)

// FuzzResolveSchemaType builds arbitrary TypeAnnotation trees from fuzz input
// and verifies that ResolveSchemaType never panics.
func FuzzResolveSchemaType(f *testing.F) {
	// Seed corpus — known-good and known-tricky inputs.
	seeds := []TypeAnnotation{
		{Kind: TypeAnnotSimple, Name: "str"},
		{Kind: TypeAnnotSimple, Name: "int"},
		{Kind: TypeAnnotSimple, Name: "dict"},
		{Kind: TypeAnnotSimple, Name: "list"},
		{Kind: TypeAnnotSimple, Name: "Any"},
		{Kind: TypeAnnotSimple, Name: "UnknownType"},
		{Kind: TypeAnnotSimple, Name: ""},
		{Kind: TypeAnnotGeneric, Name: "dict", Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
			{Kind: TypeAnnotSimple, Name: "int"},
		}},
		{Kind: TypeAnnotGeneric, Name: "list", Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
		}},
		{Kind: TypeAnnotGeneric, Name: "Optional", Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
		}},
		{Kind: TypeAnnotGeneric, Name: "Iterator", Args: []TypeAnnotation{
			{Kind: TypeAnnotGeneric, Name: "dict", Args: []TypeAnnotation{
				{Kind: TypeAnnotSimple, Name: "str"},
				{Kind: TypeAnnotGeneric, Name: "list", Args: []TypeAnnotation{
					{Kind: TypeAnnotSimple, Name: "int"},
				}},
			}},
		}},
		{Kind: TypeAnnotUnion, Args: []TypeAnnotation{
			{Kind: TypeAnnotSimple, Name: "str"},
			{Kind: TypeAnnotSimple, Name: "None"},
		}},
	}

	// Add byte-encoded seeds.
	for _, s := range seeds {
		b := encodeAnnotation(s)
		f.Add(b)
	}

	ctx := NewImportContext()
	models := NewOrderedMap[string, []ModelField]()

	f.Fuzz(func(t *testing.T, data []byte) {
		ann, _ := decodeAnnotation(data, 0, 0)
		// Must not panic regardless of input.
		st, err := ResolveSchemaType(ann, ctx, models)
		if err == nil {
			// If resolution succeeded, JSONSchema must not panic.
			_ = st.JSONSchema()
		}
	})
}

// FuzzJSONSchema constructs random SchemaType trees and ensures
// JSONSchema() never panics.
func FuzzJSONSchema(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1})
	f.Add([]byte{2, 0, 3, 's', 't', 'r'})
	f.Add([]byte{3, 2, 0, 3, 's', 't', 'r'})
	f.Add([]byte{4, 1, 2, 0, 3, 'i', 'n', 't'})

	f.Fuzz(func(t *testing.T, data []byte) {
		st, _ := decodeSchemaType(data, 0, 0)
		// Must not panic.
		_ = st.JSONSchema()
		_ = st.jsonSchema(false)
	})
}

// ---------------------------------------------------------------------------
// Annotation encoder/decoder — deterministic mapping from bytes to trees.
// ---------------------------------------------------------------------------

const maxFuzzDepth = 8

// encodeAnnotation serializes a TypeAnnotation to bytes.
func encodeAnnotation(ann TypeAnnotation) []byte {
	buf := append([]byte{byte(ann.Kind), byte(len(ann.Name))}, []byte(ann.Name)...)
	buf = append(buf, byte(len(ann.Args)))
	for _, a := range ann.Args {
		buf = append(buf, encodeAnnotation(a)...)
	}
	return buf
}

// decodeAnnotation deserializes bytes into a TypeAnnotation tree.
// Returns the annotation and number of bytes consumed.
func decodeAnnotation(data []byte, offset int, depth int) (TypeAnnotation, int) {
	if depth > maxFuzzDepth || offset >= len(data) {
		return TypeAnnotation{Kind: TypeAnnotSimple, Name: "str"}, offset
	}

	kind := TypeAnnotationKind(data[offset] % 3)
	offset++

	// Read name length and name.
	nameLen := 0
	if offset < len(data) {
		nameLen = int(data[offset]) % 32 // cap name length
		offset++
	}
	if offset+nameLen > len(data) {
		nameLen = len(data) - offset
	}
	name := string(data[offset : offset+nameLen])
	offset += nameLen

	// Read args count.
	numArgs := 0
	if offset < len(data) {
		numArgs = int(data[offset]) % 4 // cap at 3 args
		offset++
	}

	var args []TypeAnnotation
	for i := 0; i < numArgs && offset < len(data); i++ {
		arg, newOffset := decodeAnnotation(data, offset, depth+1)
		args = append(args, arg)
		offset = newOffset
	}

	return TypeAnnotation{Kind: kind, Name: name, Args: args}, offset
}

// decodeSchemaType builds a SchemaType tree from bytes.
func decodeSchemaType(data []byte, offset int, depth int) (SchemaType, int) {
	if depth > maxFuzzDepth || offset >= len(data) {
		return SchemaPrim(TypeString), offset
	}

	kind := SchemaTypeKind(data[offset] % 7)
	offset++

	switch kind {
	case SchemaPrimitive:
		prim := PrimitiveType(0)
		if offset < len(data) {
			prim = PrimitiveType(data[offset] % 9)
			offset++
		}
		st := SchemaPrim(prim)
		if offset < len(data) && data[offset]%2 == 1 {
			st.Nullable = true
		}
		if offset < len(data) {
			offset++
		}
		return st, offset

	case SchemaAny:
		return SchemaAnyType(), offset

	case SchemaArray:
		items, newOffset := decodeSchemaType(data, offset, depth+1)
		return SchemaArrayOf(items), newOffset

	case SchemaDict:
		val, newOffset := decodeSchemaType(data, offset, depth+1)
		return SchemaDictOf(val), newOffset

	case SchemaObject:
		numFields := 0
		if offset < len(data) {
			numFields = int(data[offset]) % 5
			offset++
		}
		fields := NewOrderedMap[string, SchemaField]()
		for i := 0; i < numFields && offset < len(data); i++ {
			nameLen := int(data[offset]) % 8
			offset++
			if offset+nameLen > len(data) {
				nameLen = len(data) - offset
			}
			name := string(data[offset : offset+nameLen])
			offset += nameLen
			ft, newOffset := decodeSchemaType(data, offset, depth+1)
			required := false
			if newOffset < len(data) {
				required = data[newOffset]%2 == 0
				newOffset++
			}
			fields.Set(name, SchemaField{Type: ft, Required: required})
			offset = newOffset
		}
		return SchemaObjectOf(fields), offset

	case SchemaIterator:
		elem, newOffset := decodeSchemaType(data, offset, depth+1)
		return SchemaIteratorOf(elem), newOffset

	case SchemaConcatIterator:
		return SchemaConcatIteratorOf(), offset

	default:
		return SchemaPrim(TypeString), offset
	}
}
