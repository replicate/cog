---
# cog-31yg
title: Consolidate weight config intent into WeightSpec
status: completed
type: task
priority: high
created_at: 2026-04-23T17:09:00Z
updated_at: 2026-04-23T20:59:14Z
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

- [x] Expand `WeightSpec` type with `URI`, `Include`, `Exclude`
- [x] Add `WeightSpecFromConfig(config.WeightSource) (*WeightSpec, error)` constructor
- [x] Add `WeightSpecFromLock(WeightLockEntry) *WeightSpec` constructor
- [x] Add `Equal(other *WeightSpec) bool` method
- [x] Update `source.go` and `resolver.go` construction sites
- [x] Update `weight_builder.go` to use spec fields; simplify cache-hit stamping
- [x] Replace `isStale()` in `weights_status.go` with spec comparison
- [x] Update all affected tests
- [x] Verify `go test ./...` and `mise run lint:go` clean



## Summary of Changes

Consolidated weight config intent into a single `WeightSpec` type that is loadable from either cog.yaml (via `WeightSpecFromConfig`) or the lockfile (via `WeightSpecFromLock`) and directly comparable with `Equal`. This replaces scattered field-by-field drift detection across three representations.

**Key decisions:**

- **Include/Exclude are sets, not sequences.** `WeightSpecFromConfig` sorts them at construction; reordering patterns in cog.yaml is not drift.
- **`WeightSpecFromLock` reads as-stored, no normalization.** Any deviation from canonical form on disk reports stale and triggers a rewrite on the next build.
- **Malformed configs fail at spec construction** (empty or invalid-scheme URI). Previously malformed specs could propagate into the builder, status check, and lockfile.
- **Fixed cache-hit bug**: the builder now stamps the full spec on cache hit (target, URI, include, exclude), not just target and URI. Include/exclude changes in cog.yaml are no longer silently swallowed.
- **`ArtifactSpecs` now returns `(specs, error)`** so malformed weight URIs fail loudly instead of producing an incomplete artifact set.

**Files changed:**
- `pkg/model/artifact_weight.go`: expanded `WeightSpec`, added constructors + `Equal`, removed `NewWeightSpec`.
- `pkg/model/source.go`, `pkg/model/resolver.go`: use `WeightSpecFromConfig`.
- `pkg/model/weight_builder.go`: cache-hit stamps full spec; cache-miss consumes spec fields directly.
- `pkg/model/weights_status.go`: `isStale` replaced with `WeightSpec.Equal` comparison; removed `normalizeConfigURI`, `normalizeInclude`, `normalizeExclude` helpers.
- `pkg/model/weights_lock.go`: updated `WeightLockSource.Include`/`Exclude` doc to reflect sorted-set semantics.
- `pkg/cli/weights.go`: handles new error return from `ArtifactSpecs`.
- Tests: added coverage for `WeightSpecFromConfig`, `WeightSpecFromLock`, `Equal` (including URI normalization, reorder-ignoring, and stale-on-non-canonical-lockfile).

Committed as e4a80533.
