---
# cog-6wm0
title: Include/exclude filters for weight import
status: todo
type: task
priority: high
created_at: 2026-04-17T19:27:37Z
updated_at: 2026-04-23T21:29:58Z
parent: cog-66gt
---

Add include/exclude glob filtering to the weight import pipeline.

In cog.yaml:
  weights:
    - name: z-image-turbo
      target: /src/weights
      source:
        uri: hf://stabilityai/z-image-turbo
        include: ["*.safetensors", "*.json"]
        exclude: ["*.onnx", "*.bin", "*.msgpack"]

Motivation: HF repos routinely ship the same weights in multiple formats
(.safetensors, .bin, .onnx, .gguf, .h5, original/). Without filtering, an
hf:// import pulls everything — e.g. sentence-transformers/all-MiniLM-L6-v2
is ~930MB of duplicate representations when the user typically wants
~30MB of safetensors + tokenizer. The same problem exists for file://
sources to a lesser degree.

## Semantics

Pattern matching is gitignore-style via `github.com/sabhiram/go-gitignore`
(already a project dependency, used by `pkg/dockerignore`). This matches
most developers' mental model: bare patterns float ("*.bin" matches any
.bin file at any depth), path-shaped patterns anchor ("onnx/*.bin" only
matches direct children of onnx/), leading slash forces root anchoring.

### Inclusion rule

A file is part of the weight set if:

1. If `include` is non-empty, the file's path matches at least one
   include pattern. If `include` is empty or absent, this check is
   skipped (all files pass).
2. The file's path does not match any `exclude` pattern.

Exclude takes precedence over include — a file matching both is excluded.
This matches HF, gitignore, and dockerignore conventions.

### Pattern validation (config parse time)

- Empty-string patterns are rejected with a clear error.
- `!`-prefixed patterns (gitignore negation) are rejected. No negation in v1.
- Patterns are trimmed of leading/trailing whitespace.
- Patterns use forward slashes on all platforms, regardless of host OS.
- Matching is case-sensitive.
- Comments are not supported in patterns — YAML already allows comments
  at the document level.

### Ordering

Pattern order within a list is not significant (no negation = match
is commutative).

### Zero-survivor behavior

If the filter yields zero files, the build errors with a message showing
the inventory size and the patterns applied. An empty weight set is
almost always a mistake and should surface immediately, not at push time.

### Patterns that match nothing in the source

Not an error and not a warning. Users commonly maintain canonical exclude
lists across multiple sources, some of which won't have the ignored file
types.

## Fingerprint + lockfile semantics

The source fingerprint is the upstream version identity only (e.g.
`commit:<sha>` for hf://, `sha256:<hex>` of the full file set for
file://). It does NOT change when include/exclude change — the upstream
hasn't changed.

The lockfile records the resolved upstream fingerprint alongside the
applied include/exclude patterns. A pattern change therefore invalidates
the cache via the existing drift detection in `weights_status.go:219-222`
(already tested at `weights_status_test.go:189-260`).

This split is important: it lets us distinguish "upstream moved" from
"user narrowed/widened what to import" as separate causes of re-import.

## Compatibility note for HF users

Users copying patterns from HF docs or the `hf` CLI will find the common
cases behave identically (`*.safetensors`, `*.bin`, `onnx/*` all work the
same way). One deviation: HF's fnmatch lets `*` cross `/`, so
`onnx/*.bin` on HF matches `x/onnx/foo.bin` too. Gitignore (and our
semantics) don't — `*` stays within a path segment. This is the safer
direction and rarely trips anyone up in practice.

## Implementation plan

### New

- `pkg/model/weightsource/filter.go` — `filterInventory(files, include, exclude)` helper. Pure function over `[]InventoryFile`. Use `ignore.CompileIgnoreLines` once per list, then `MatchesPath` per file. Include/exclude rule as specified above.
- `pkg/model/weightsource/filter_test.go` — exhaustive table-driven tests covering the edge cases from this spec: empty inputs, precedence, zero-survivors, floating vs. anchored, directory patterns, `**` recursion, case sensitivity, forward-slash-only.

### Plumbing

- `pkg/config/validate.go` — reject empty-string and `!`-prefixed patterns in `WeightSourceConfig`. Trim whitespace. Per-pattern error with index and the offending value.
- `pkg/model/ref_types.go` — add `Include []string` and `Exclude []string` to `WeightSpec` (or wherever the spec struct lives; currently `NewWeightSpec` only takes name/uri/target).
- `pkg/model/source.go:89` — pass `w.Source.Include` and `w.Source.Exclude` from config into `NewWeightSpec`.
- `pkg/model/weight_builder.go` — after `src.Inventory(ctx)` at line ~99, apply `filterInventory` with the spec's Include/Exclude. At line ~131–136, replace the hardcoded empty slices with the actual patterns from the spec. Error on zero survivors with inventory size + patterns in the message.

### Out of scope (track as follow-ups)

- `--dry-run` / `--verbose` / `cog weights inspect` UX for previewing the filter result. Likely a separate bean.
- Per-scheme smart defaults (e.g. auto-excluding `original/` for hf://). Revisit after real usage; explicit patterns for v1.
- `!` negation. Explicitly out.
- `.cogignore` file as an alternative to inline YAML patterns. Not planned.

## Todo

- [ ] Write filterInventory + table-driven tests
- [ ] Add Include/Exclude to WeightSpec
- [ ] Thread patterns from config through Source.ArtifactSpecs → WeightSpec → builder
- [ ] Apply filter in weight_builder.Build after Inventory, before pack
- [ ] Record applied patterns in WeightLockSource (replace hardcoded []string{})
- [ ] Config-time validation: reject empty/!-prefixed patterns, trim whitespace
- [ ] Zero-survivor error with actionable message
- [ ] Docs: cog.yaml reference section on include/exclude semantics + HF compatibility note
- [ ] Integration test: hf:// source with include/exclude producing a smaller, correct weight set

## Dependency on s5fy (2026-04-21)

s5fy added `include` and `exclude` fields to the lockfile's `source` block (`WeightLockSource` type). They are always serialized as `[]` when empty (never omitted), establishing the schema. s5fy did NOT apply filtering — it just records the patterns from cog.yaml into the lockfile (currently always empty; see weight_builder.go:134-135).

This bean implements the actual filtering and wiring.

## Resolution of original open question

Original bean asked: "whether we need both include AND exclude, or just exclude with gitignore-style negation patterns."

**Resolved: both include AND exclude, no negation.**

Reasoning: matches HF's `allow_patterns`/`ignore_patterns`, matches dockerignore's include/exclude pairs, and keeps semantics tractable. Negation (`!` re-include on top of exclude) is a power-user feature with confusing ordering rules; we can add it later if users ask.
