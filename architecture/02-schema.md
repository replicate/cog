# Schema

The schema is an **OpenAPI 3.0.2 specification** that describes a model's interface. It's the contract between the model and everything that interacts with it.

## Why the Schema Exists

Every Cog model uses the same [Prediction API](./03-prediction-api.md) envelope format, but the `input` and `output` fields are model-specific. The schema captures what each model expects and produces.

```mermaid
flowchart TB
    subgraph envelope ["PredictionRequest (fixed envelope)"]
        input["&quot;input&quot;#colon; { ... } — model-specific"]
    end
    envelope -.- note["Schema defines this part"]
```

Without the schema, consumers would have no way to know:
- What inputs the model accepts
- What types those inputs should be
- What constraints apply (required fields, min/max values, allowed choices)
- What the output looks like

### How It's Used Today

| Consumer | What They Use the Schema For |
|----------|------------------------------|
| **Replicate platform** | Generate input forms in the web UI, validate requests before routing to models |
| **HTTP server (coglet)** | Validate incoming JSON, reject malformed requests before they reach user code |
| **CLI (`cog predict`)** | Parse `-i key=value` flags into correctly-typed Python objects |
| **Docker label** | Extract model interface without running the container |
| **API clients** | Know what to send and what to expect back without reading source code |

## How It's Generated

Cog supports two schema generation paths:

### Static Path (default)

The **static path** parses Python source code at `cog build` time using [tree-sitter](https://tree-sitter.github.io/tree-sitter/) in Go. No Python runtime is invoked, no container boots — the schema is produced from the model's source files before Docker build even begins. This makes schema generation deterministic, fast, and independent of the model's runtime dependencies. Static generation is the default for all commands and requires SDK >= 0.17.0.

If the static parser encounters a type it can't resolve (for example, a `BaseModel` subclass exported from a package `__init__.py` that the file-based resolver can't find), it **falls back to the runtime path** automatically with a warning — so builds don't break due to static-parser limitations. Hard user errors (parse errors, unsupported features like `default_factory`) still fail fast with a clear static-generator error rather than being swept into the fallback.

### Runtime Path (legacy fallback)

The **runtime path** boots the built Docker container and runs `python -m cog.command.openapi_schema` to introspect the model at runtime. It uses Python's `inspect` module and `typing.get_type_hints()` to extract parameter types, then builds OpenAPI JSON from a hand-rolled ADT type system (dataclasses in `_adt.py`). No pydantic is involved in schema generation -- `cog.BaseModel` is a dataclass wrapper, not pydantic.

The runtime path is the default for SDK versions < 0.17.0 (pydantic-based schemas the static parser cannot analyze) and for the static-path fallback described above. It can be forced globally by setting `COG_LEGACY_SCHEMA=1`:

```bash
COG_LEGACY_SCHEMA=1 cog build -t my-model
```

This opt-out exists as a lifeline for users pinned to old SDKs or hitting static-parser bugs that haven't been resolved yet.

Local commands (`cog serve`, `cog predict`, `cog train`) need a schema to parse `-i` input flags into correctly-typed Python objects. Because those commands run before the post-build legacy runtime generation step, they only work when the static path produced a schema — setting `COG_LEGACY_SCHEMA=1` for a `cog predict` build leaves the image without a bundled schema and `-i` parsing is unavailable.

```mermaid
flowchart LR
    subgraph source["Model Source"]
        predict["predict.py"]
        types["output_types.py"]
    end

    subgraph parser["Go Static Parser"]
        ts["tree-sitter Python"]
        resolve["Type Resolver"]
        cross["Cross-File Resolver"]
    end

    subgraph output["Schema"]
        spec["OpenAPI 3.0.2 JSON"]
    end

    predict --> ts
    types --> cross
    ts --> resolve
    cross --> resolve
    resolve --> spec
```

### Static Path Pipeline Steps

1. **Parse** the predictor file with tree-sitter (concrete syntax tree, not AST)
2. **Collect imports** -- track where each name came from (`from cog import Path`, `from cog import BaseModel`)
3. **Collect module scope** -- resolve module-level variable assignments (for default values, choices lists)
4. **Collect BaseModel subclasses** -- find all classes that inherit from `BaseModel` (cog's dataclass-based version; pydantic BaseModel also supported for compatibility)
5. **Resolve cross-file models** — for imported names not found locally, find the `.py` file on disk, parse it, and extract its BaseModel definitions
6. **Extract inputs** — walk the `predict()` method parameters, resolve types, defaults, and `Input()` metadata
7. **Resolve output type** — recursively resolve the return type annotation into a `SchemaType`
8. **Generate OpenAPI** — convert the extracted `PredictorInfo` into a full OpenAPI 3.0.2 JSON document

If any step fails with an unresolvable type, the build falls back to the runtime path.

### Cross-File Resolution

When a predictor imports types from other project files, the schema generator resolves them automatically:

```python
# output_types.py
from cog import BaseModel

class Prediction(BaseModel):
    text: str
    score: float
    tags: list[str]
```

```python
# predict.py
from cog import BasePredictor
from output_types import Prediction

class Predictor(BasePredictor):
    def predict(self, prompt: str) -> Prediction:
        ...
```

The resolver handles every permutation of local imports:

| Import Style | File Resolved |
|-------------|---------------|
| `from output_types import X` | `<project>/output_types.py` |
| `from .output_types import X` | `<project>/output_types.py` |
| `from models.output import X` | `<project>/models/output.py` |
| `from .models.output import X` | `<project>/models/output.py` |
| `from output_types import X as Y` | `<project>/output_types.py` (alias tracked) |

**How it distinguishes local from external**: the resolver converts the module path to a filesystem path and checks if the file exists. If `output_types.py` exists in the project directory, it's local. If not (e.g., `from transformers import ...`), it's external. Known external packages (stdlib, torch, numpy, etc.) are skipped without a filesystem check.

**Error messages**: when a type can't be resolved, the error includes the import source:

```text
cannot resolve output type 'WeirdType' (imported from 'some_package') —
external types cannot be statically analyzed. Define it as a BaseModel
subclass in your predict file, or provide a .pyi stub
```

## SchemaType: The Type System

Output types are represented as a recursive algebraic data type (`SchemaType`) that composes arbitrarily:

```mermaid
flowchart TD
    root["SchemaType"] --> prim["SchemaPrimitive — str, int, float, bool, Path"]
    root --> any["SchemaAny — untyped (bare dict, Any)"]
    root --> arr["SchemaArray — list#lsqb;T#rsqb;, with Items → SchemaType"]
    root --> dict["SchemaDict — dict#lsqb;str, V#rsqb;, with ValueType → SchemaType"]
    root --> obj["SchemaObject — BaseModel subclass, with Fields → OrderedMap"]
    root --> iter["SchemaIterator — Iterator#lsqb;T#rsqb;, with Elem → SchemaType"]
    root --> concat["SchemaConcatIterator — ConcatenateIterator#lsqb;str#rsqb;"]
```

This recursive structure means nested types like `dict[str, list[dict[str, int]]]` are fully representable and produce correct JSON Schema:

```json
{
  "type": "object",
  "additionalProperties": {
    "type": "array",
    "items": {
      "type": "object",
      "additionalProperties": {
        "type": "integer"
      }
    }
  }
}
```

### JSON Schema Generation

Each `SchemaType` produces its JSON Schema fragment via `JSONSchema()`:

| SchemaType Kind | JSON Schema |
|-----------------|-------------|
| `SchemaPrimitive(str)` | `{"type": "string"}` |
| `SchemaPrimitive(Path)` | `{"type": "string", "format": "uri"}` |
| `SchemaAny` | `{"type": "object"}` |
| `SchemaArray(items)` | `{"type": "array", "items": items.JSONSchema()}` |
| `SchemaDict(valueType)` | `{"type": "object", "additionalProperties": valueType.JSONSchema()}` |
| `SchemaObject(fields)` | `{"type": "object", "properties": {...}, "required": [...]}` |
| `SchemaIterator(elem)` | `{"type": "array", "items": elem.JSONSchema(), "x-cog-array-type": "iterator"}` |
| `SchemaConcatIterator` | `{"type": "array", "items": {"type": "string"}, "x-cog-array-type": "iterator", "x-cog-array-display": "concatenate"}` |

## Type Mappings

### Input Types

| Python | JSON Schema | Notes |
|--------|-------------|-------|
| `str` | `{"type": "string"}` | |
| `int` | `{"type": "integer"}` | |
| `float` | `{"type": "number"}` | |
| `bool` | `{"type": "boolean"}` | |
| `cog.Path` | `{"type": "string", "format": "uri"}` | URLs downloaded at runtime |
| `cog.File` | `{"type": "string", "format": "uri"}` | File uploads |
| `cog.Secret` | `{"type": "string", "format": "password", "x-cog-secret": true}` | Masked in logs |
| `list[T]` | `{"type": "array", "items": {...}}` | |
| `Optional[T]` | Type T + not in `required` | Input fields only |
| `Literal["a", "b"]` / `choices=[...]` | `{"enum": ["a", "b"]}` | |

### Output Types

| Python | SchemaType | JSON Schema |
|--------|------------|-------------|
| `str` | `SchemaPrimitive` | `{"type": "string"}` |
| `int` | `SchemaPrimitive` | `{"type": "integer"}` |
| `float` | `SchemaPrimitive` | `{"type": "number"}` |
| `bool` | `SchemaPrimitive` | `{"type": "boolean"}` |
| `Path` | `SchemaPrimitive` | `{"type": "string", "format": "uri"}` |
| `dict` (bare) | `SchemaAny` | `{"type": "object"}` |
| `dict[str, V]` | `SchemaDict` | `{"type": "object", "additionalProperties": V}` |
| `list` (bare) | `SchemaArray(SchemaAny)` | `{"type": "array", "items": {"type": "object"}}` |
| `list[T]` | `SchemaArray` | `{"type": "array", "items": T}` |
| `BaseModel` subclass | `SchemaObject` | `{"type": "object", "properties": {...}}` |
| `Iterator[T]` | `SchemaIterator` | `{"type": "array", "items": T, "x-cog-array-type": "iterator"}` |
| `ConcatenateIterator[str]` | `SchemaConcatIterator` | Streaming token output |
| Nested types | Recursive | `dict[str, list[dict[str, int]]]` fully supported |

### Unsupported Output Types

| Python | Error |
|--------|-------|
| `Optional[T]` / `T \| None` | Predictions must succeed with a value or fail with an error |
| `Union[A, B]` | Ambiguous for downstream consumers |
| External package types | Cannot be statically analyzed — define as BaseModel or use .pyi stub |

## Cog-Specific Extensions

| Extension | Purpose |
|-----------|---------|
| `x-order` | Preserves parameter order from function signature |
| `x-cog-array-type` | Marks iterators vs regular arrays |
| `x-cog-array-display` | Hints for how to display streaming output |
| `x-cog-secret` | Marks sensitive inputs |

## Where the Schema Lives

### In the Image

Embedded as a Docker label during build:

```bash
docker inspect my-model | jq -r '.[0].Config.Labels["run.cog.openapi_schema"]'
```

Also written to `.cog/openapi_schema.json` inside the image for the runtime to serve.

### At Runtime

| Endpoint | Format |
|----------|--------|
| `GET /openapi.json` | Raw OpenAPI spec |

### Override and Configuration

| Environment Variable | Purpose |
|---------------------|---------|
| `COG_LEGACY_SCHEMA=1` | Force the legacy runtime schema path (opt-out of the default static generator). Use when pinned to SDK < 0.17.0 or hitting a static-parser bug. |
| `COG_OPENAPI_SCHEMA=path` | Skip generation entirely and use a pre-built schema file. |

```bash
# Default: static schema generation, falls back to runtime on parser limits
cog build -t my-model

# Force the legacy runtime path (pinned to old SDK, debugging static parser, etc.)
COG_LEGACY_SCHEMA=1 cog build -t my-model

# Use a pre-built schema file
COG_OPENAPI_SCHEMA=my_schema.json cog build
```

## Schema Structure

A simplified example showing a multi-file predictor with structured output:

```json
{
  "openapi": "3.0.2",
  "info": { "title": "Cog", "version": "0.1.0" },
  "paths": {
    "/predictions": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/PredictionRequest" }
            }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Input": {
        "type": "object",
        "properties": {
          "prompt": {
            "type": "string",
            "description": "Text prompt",
            "x-order": 0
          },
          "steps": {
            "type": "integer",
            "default": 50,
            "minimum": 1,
            "maximum": 100,
            "x-order": 1
          }
        },
        "required": ["prompt"]
      },
      "Output": {
        "type": "object",
        "properties": {
          "text": { "type": "string", "title": "Text" },
          "score": { "type": "number", "title": "Score" }
        },
        "required": ["text", "score"]
      },
      "PredictionRequest": { "..." : "..." },
      "PredictionResponse": { "..." : "..." }
    }
  }
}
```

## Code References

| File | Purpose |
|------|---------|
| `pkg/schema/schema_type.go` | `SchemaType` ADT, `ResolveSchemaType()`, `JSONSchema()` generation |
| `pkg/schema/types.go` | `PredictorInfo`, `PrimitiveType`, `FieldType`, `InputField`, `ImportContext` |
| `pkg/schema/python/parser.go` | Tree-sitter Python parser, `ParsePredictor()`, cross-file resolution |
| `pkg/schema/openapi.go` | OpenAPI document assembly from `PredictorInfo` |
| `pkg/schema/generator.go` | Top-level `Generate()`, `GenerateCombined()`, `Parser` type |
| `pkg/schema/errors.go` | Typed error kinds (`ErrUnresolvableType`, `ErrOptionalOutput`, etc.) |
| `pkg/image/build.go` | `canUseStaticSchemaGen()` -- opt-in gate, `generateStaticSchema()` -- entry point, fallback to runtime path on `ErrUnresolvableType` |
| `pkg/image/openapi_schema.go` | `GenerateOpenAPISchema()` -- runtime path (boots container, runs `python -m cog.command.openapi_schema`) |
| `python/cog/_adt.py` | Internal ADT types for Python-side predictor introspection |
| `python/cog/_inspector.py` | Python-side predictor inspector (runtime introspection) |
| `python/cog/_schemas.py` | Python-side OpenAPI schema generation from inspected predictor info |
| `python/cog/command/openapi_schema.py` | CLI entry point for `python -m cog.command.openapi_schema` (invoked by runtime path) |
