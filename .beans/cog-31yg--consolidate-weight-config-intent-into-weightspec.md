---
# cog-31yg
title: Consolidate weight config intent into WeightSpec
status: todo
type: task
priority: high
created_at: 2026-04-23T17:09:00Z
updated_at: 2026-04-23T17:09:00Z
parent: cog-66gt
---

## Why

Config-driven fields (target, URI, include, exclude) are scattered across three representations: `config.WeightSource`, `WeightSpec`, and `WeightLockEntry`. The builder manually threads fields between them, and the cache-hit path must remember to stamp each one individually. This caused a bug where changing `target` in cog.yaml was silently swallowed on cache hit. Adding any new config field requires updating multiple sites — there is no single "config intent" type.

## Design

Make `WeightSpec` the canonical "what the user asked for" type — loadable from either cog.yaml or the lockfile, directly comparable.

### Type

```go
type WeightSpec struct {
    name    string   // immutable, unexported with Name() accessor
    Target  string   // container mount path
    URI     string   // normalized source URI (file://./weights, hf://org/repo)
    Include []string // normalized glob patterns (nil → []string{})
    Exclude []string // normalized glob patterns (nil → []string{})
}
```

Five fields. All normalized at construction time. No computed/derived fields (digests, fingerprints, layers — those stay on `WeightLockEntry`).

### Constructors

```go
// From cog.yaml — normalizes URI, copies and normalizes include/exclude.
// Does not need projectDir (NormalizeURI operates on the URI string alone).
func WeightSpecFromConfig(w config.WeightSource) (*WeightSpec, error)

// From lockfile — extracts the config-intent subset.
func WeightSpecFromLock(e WeightLockEntry) *WeightSpec
```

### Comparison

```go
// Equal reports whether two specs describe the same user intent.
// Compares Target, URI, Include, Exclude. Name is excluded (you would
// never compare specs with different names).
func (s *WeightSpec) Equal(other *WeightSpec) bool
```

### projectDir

Does NOT go on WeightSpec. `projectDir` is pure runtime context — never enters identity fields (URI, fingerprint, digests). It stays on `WeightBuilder`/`Source` where the builder passes it to `weightsource.For()` for filesystem resolution.

### ArtifactSpec interface

No change. `WeightSpec` still satisfies `ArtifactSpec` via `Type()` + `Name()`.

## Impact

| File | Change |
|------|--------|
| `pkg/model/artifact_weight.go` | Expand `WeightSpec` with `URI`, `Include`, `Exclude`. Add `WeightSpecFromConfig`, `WeightSpecFromLock`, `Equal`. Remove `Source string` field (replaced by `URI`). |
| `pkg/model/source.go` | Update `ArtifactSpecs()` to use `WeightSpecFromConfig` |
| `pkg/model/resolver.go` | Update `Build()` to use `WeightSpecFromConfig` |
| `pkg/model/weight_builder.go` | Simplify cache-hit path: replace manual field stamping with `Equal` check. Read `spec.URI` instead of `spec.Source`. Cache-miss path: construct `WeightLockSource` from spec fields. |
| `pkg/model/weights_status.go` | Replace `isStale()` with `configSpec.Equal(lockSpec)` using `WeightSpecFromConfig` and `WeightSpecFromLock` |
| `pkg/model/weights_lock.go` | May simplify `lockEntriesSourceEqual` — the config-intent comparison moves to `WeightSpec.Equal` |
| Tests across all of the above | |

## Invariants to preserve

- `Include`/`Exclude` normalized to `[]string{}` (never nil) at construction time, both from config and from lockfile
- Lockfile serializes `[]` not `null` for empty include/exclude
- `lockEntriesEqual` still needed for the "should we rewrite the lockfile" check (compares content fields too, not just config intent)

## Todo

- [ ] Expand `WeightSpec` type with `URI`, `Include`, `Exclude`
- [ ] Add `WeightSpecFromConfig(config.WeightSource) (*WeightSpec, error)` constructor
- [ ] Add `WeightSpecFromLock(WeightLockEntry) *WeightSpec` constructor
- [ ] Add `Equal(other *WeightSpec) bool` method
- [ ] Update `source.go` and `resolver.go` construction sites
- [ ] Update `weight_builder.go` to use spec fields; simplify cache-hit stamping
- [ ] Replace `isStale()` in `weights_status.go` with spec comparison
- [ ] Update all affected tests
- [ ] Verify `go test ./...` and `mise run lint:go` clean
