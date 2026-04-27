---
# cog-i12u
title: Warm local WeightStore during cog weights import
status: completed
type: task
priority: high
created_at: 2026-04-27T19:01:19Z
updated_at: 2026-04-27T21:40:36Z
parent: cog-66gt
---

Make `cog weights import` populate the local content-addressed store at `$XDG_CACHE_HOME/cog/weights/files/sha256/…` so `cog predict` can hardlink-assemble locally without a separate `cog weights pull`. Add a top-level `envelopeFormat` digest field to the lockfile that identifies the packer configuration that produced (or will produce) the recorded layer digests. Drop the `.cog/weights-cache/` on-disk tar scratch entirely.

User-facing contract becomes: `weights import` ≡ `weights import + weights pull`. `weights pull` stays for the case where someone has a checked-in `weights.lock` but didn't run import locally.

## Why

Today `cog weights import` does build + push but leaves the local cache cold. `cog predict` then refuses to run with "weight not fully cached locally; run 'cog weights pull' first." Two commands to do what should be one. The local store is already content-addressed and ready to be populated during import — we just weren't doing it.

While we're in there, the `.cog/weights-cache/` tar directory is conflating "skip the pack" with "skip the work" — but the pack itself is cheap and deterministic given the file digests. The expensive part is source ingress, which the WeightStore handles. Removing the tar cache simplifies the design.

The lockfile gains an `envelopeFormat` digest so we can detect when a cog upgrade changes packer behavior in a way that requires recompute, without having to recompute on every import.

## Goal (what changes for users)

After `cog weights import` succeeds:
- Every file just imported is in the local store, content-addressed.
- `cog predict` works immediately, hardlink-assembling from the store.
- A no-op import (nothing changed) is fast: source walk + a handful of stat + HEAD calls.
- A real-change import re-streams only the affected layers and pushes only what's not already on the registry.

## Design

### Lockfile schema change

Add top-level `envelopeFormat` string field:

```jsonc
{
  "version": 2,
  "envelopeFormat": "sha256:<hex>",
  "weights": [ ... ]
}
```

`envelopeFormat` is the sha256 of canonical JSON of the `envelope` struct (parameters + revision). Always written on every lockfile rewrite; mismatch on read forces a recompute pass. No backcompat shim — missing field = mismatch = recompute on first import.

### Envelope identity (new file: `pkg/model/envelope.go`)

```go
const envelopeRevision = 1

type envelope struct {
    BundleFileMax       int64    `json:"bundleFileMax"`
    BundleSizeMax       int64    `json:"bundleSizeMax"`
    GzipLevelBundle     int      `json:"gzipLevelBundle"`
    GzipLevelLarge      int      `json:"gzipLevelLarge"`
    IncompressibleExts  []string `json:"incompressibleExts"` // sorted
    MediaTypeCompressed string   `json:"mediaTypeCompressed"`
    MediaTypeRaw        string   `json:"mediaTypeRaw"`
    Revision            int      `json:"revision"`
}

func envelopeFromOptions(opts packOptions) envelope { ... }
func ComputeEnvelopeFormat(env envelope) (string, error) { ... }
```

A SYNC comment block at the `envelopeRevision` constant lists what triggers a manual bump (tar header format, file ordering, compressor framing, default flips for existing parameters). Pure parameter changes (thresholds, levels, media types, ext lists) are caught automatically by the struct fields.

### Snapshot tests (new file: `pkg/model/envelope_test.go`)

- `TestEnvelopeFormat_DefaultIsStable` — anchors the digest of `envelopeFromOptions(packOptions{})`. On failure, a long help message (in a `const envelopeFormatChangeMessage` next to the snapshot) explains:
  - What the change implies (lockfile churn on next import).
  - When it's intentional (parameter change, revision bump).
  - When it's unintentional (revert).
  - When to bump `envelopeRevision` (bytes change but parameter struct didn't catch it).
- Table-driven snapshots for a few non-default envelopes (custom bundle size, bumped revision).
- Determinism (marshal twice, same digest).
- Revision-bump-changes-digest sanity check.

The point is: anyone — human, coding agent, code reviewer — who hits the failure has everything they need to understand what to do without leaving the test file.

### Build flow (new shape for `WeightBuilder.Build`)

```
1. Source.Inventory(ctx) — always. Import is a source read by definition.

2. Ingress: for each file in inventory:
     If !store.Exists(file.Digest):
         rc = source.Open(file.Path)
         store.PutFile(file.Digest, file.Size, rc)  // hash-verifies

3. Compute setDigest from inventory.

4. Comparison phase:
     currentEnvelope = ComputeEnvelopeFormat(envelopeFromOptions(packerOpts))

     fastPathOK = (
       lock.EnvelopeFormat == currentEnvelope
       AND existing != nil
       AND existing.SetDigest == newSetDigest
       AND inventory matches existing.Files (path / size / digest)
       AND existing.Source.{URI, Include, Exclude} matches spec
     )

     if fastPathOK:
       derivedLayers = existing.Layers              // trust the lockfile
     else:
       plan = packer.planLayers(inv)
       derivedLayers = computeLayerDigests(ctx, store, plan)
         // for each planned layer: stream tar through sha256+counter
         // with io.Discard as byte sink. No on-disk scratch.

     derivedEntry = newWeightLockEntry(name, target, source, files, derivedLayers)

5. Push phase (independent of comparison):
     for each layer in derivedLayers:
       if !registry.BlobExists(repo, layer.Digest):
         layer = tarball.LayerFromOpener(opener that streams from store)
         registry.WriteLayer(repo, layer)
     push manifest if !registry.ManifestExists.

6. Lockfile rewrite:
     lock.EnvelopeFormat = currentEnvelope         // always stamp
     if !EntriesEqual(existing, derivedEntry) OR EnvelopeFormat changed:
       lock.Upsert(derivedEntry)
       lock.Save()
```

### Behavior outcomes

**True no-op import**: source walk + `store.Exists` per file (stat) + fast-path comparison + `BlobExists` per layer (HEAD). No tar work, no recompute, no lockfile rewrite.

**Format-bump-only no-op (cog upgrade, no source change)**: source walk + store hits + fast path fails on `EnvelopeFormat` mismatch + recompute layer digests from store (local I/O + sha256 + gzip) + `BlobExists` hits, no push + lockfile rewrite for the new field only.

**Real change**: source walk + store ingress for changed files (hash-verified) + fast path fails + recompute affected layers + `BlobExists` miss for changed layers, push + lockfile rewrite.

## Scope

### In

- `pkg/weights/lockfile/lockfile.go` — add `EnvelopeFormat string` top-level field. Update load/save. No migration code.
- `pkg/weights/lockfile/lockfile_test.go` — round-trip tests for the new field.
- `pkg/model/envelope.go` (new) — `envelope` struct, `envelopeRevision` constant + SYNC comment, `envelopeFromOptions`, `ComputeEnvelopeFormat`.
- `pkg/model/envelope_test.go` (new) — snapshot tests with the failure-message constant, table-driven cases, determinism, revision-bump.
- `pkg/model/packer.go` — accept `store.Store`. During pack, `store.PutFile` source bytes (hash-verifying on ingress); read tar bytes from `store.Path`. Drop persistent on-disk tar scratch — tars stream through `tarball.LayerFromOpener` for push, through `sha256.New()` (with `io.Discard` sink) for digest computation.
- `pkg/model/weight_builder.go` — accept `store.Store`. Restructure `Build` around comparison-first / recompute-on-miss flow. Remove `WeightsCacheDir`, `cacheDirFor`, `resetCacheDir`, `cachedLayers`, `layerCachePath`, `digestToFilename`, all `.cog/weights-cache/` logic.
- `pkg/cli/weights.go` — construct the `FileStore` from `paths.WeightsStoreDir()` + `store.NewFileStore()` and pass it to `NewWeightBuilder`.
- `pkg/model/resolver.go:252` — thread store through the other `NewWeightBuilder` callsite.
- Test helpers across `pkg/model/*_test.go` updated for the new constructor shape; most just need a `FileStore` rooted in `t.TempDir()`.
- New builder tests: `TestWeightBuilder_PopulatesStore`, `TestWeightBuilder_CacheHit_PopulatesEmptyStore`, `TestWeightBuilder_CacheHit_StoreWarm_SkipsSourceRead`, `TestWeightBuilder_DigestMismatch_FailsLoudly`, `TestWeightBuilder_EnvelopeFormatMismatch_TriggersRecompute`, `TestWeightBuilder_StampsEnvelopeFormat`.
- `integration-tests/` — extend or add a test that runs `cog weights import` then `cog predict` with no intervening `cog weights pull`.
- `docs/managed-weights.md` (or wherever) — update text. Brief notes in `import` and `pull` long-help. Run `mise run docs:llm` and `mise run docs:cli`.

### Out

- Move weight code out of `pkg/model/` (deferred — not v2-blocking).
- Pending state file (cog-4rmi).
- Plan/execute split (cog-3p4a, narrowed by this work).
- BuildKit-based build (v2).
- DockerWeightStore (v2, cog-pqtq).
- Hiding or removing `cog weights pull`.
- Auto-pull from `cog predict`.
- Exposing packer parameters in `cog.yaml`.
- `weights verify` command (defer).

## Implementation order

1. `pkg/model/envelope.go` + `envelope_test.go`. Snapshot tests passing.
2. `pkg/weights/lockfile/lockfile.go` — add `EnvelopeFormat` field. Round-trip tests.
3. `pkg/model/packer.go` — store-aware ingress + read-from-store packing path. Streaming digest computation. Streaming opener for push.
4. `pkg/model/weight_builder.go` — new comparison-first flow. Always-stamp envelope format. Remove all `.cog/weights-cache/` machinery.
5. `pkg/cli/weights.go` and `pkg/model/resolver.go` — construct and thread the store.
6. Update test helpers across `pkg/model/*_test.go`.
7. Add new builder tests covering cache-warming, store-warm-skips-source, format mismatch, digest mismatch, format stamping.
8. Integration test for import-then-predict-without-pull.
9. Docs updates.
10. `mise run lint`, `mise run test:go`, `mise run test:integration`.

## Risks

- **Filesystem-boundary failure on predict**: existing constraint, unchanged. `os.Link` in `Mounts.assembleWeightDir` fails with EXDEV if `COG_CACHE_DIR` and `<projectDir>` are on different filesystems. After this change, "import without pull" still requires the same alignment. Worth a doc note.
- **Source drift detection becomes louder**: `PutFile` hash-verifies during import. If a source file mutates between `Inventory` and `Open` and the new bytes don't match the inventory digest, import now fails loudly instead of silently producing a tar whose member digest disagrees with the lockfile. Behavior improvement; flag in PR description.
- **`envelopeFormat` thrash across cog versions**: same dynamic as `package-lock.json` across node versions. Resolved by committing the lockfile and treating diffs as expected on cog version bumps. Worth a doc mention.

## Todo

- [x] `pkg/model/envelope.go` — struct, revision constant + SYNC comment, ComputeEnvelopeFormat
- [x] `pkg/model/envelope_test.go` — snapshot tests with failure-message constant
- [x] `pkg/weights/lockfile/lockfile.go` — add EnvelopeFormat field + tests
- [x] `pkg/model/packer.go` — store-aware ingress, streaming digest computation, streaming opener for push, drop persistent tar scratch
- [x] `pkg/model/weight_builder.go` — comparison-first flow, always-stamp envelope, remove .cog/weights-cache/ machinery
- [x] `pkg/cli/weights.go` — construct FileStore, pass to NewWeightBuilder
- [x] `pkg/model/resolver.go` — thread store through
- [x] Update test helpers across `pkg/model/*_test.go` for new constructor signature
- [x] New builder tests (PopulatesStore, CacheHit_PopulatesEmptyStore, CacheHit_StoreWarm_SkipsSourceRead, DigestMismatch_FailsLoudly, EnvelopeFormatMismatch_TriggersRecompute, StampsEnvelopeFormat)
- [x] Integration test: import-then-predict-without-pull
- [x] Update docs and regen `docs/llms.txt`, `docs/cli.md`
- [x] `mise run lint` (Go), `mise run test:go` all green; `mise run test:integration` not run locally (requires Docker test registry)

## Reference

- Design discussion (chat log): cache-warming, envelope format, snapshot testing
- Supersedes: parts of cog-3p4a (see that bean's update notes)
- Related: cog-pqtq (DockerWeightStore — clarified as parallel Manager-mode, not Store swap)


## Summary of Changes

Implements the cog-i12u design: `cog weights import` now warms the local content-addressed weight store as a side effect, so `cog predict` works immediately without a separate `cog weights pull`.

**New code**
- `pkg/model/envelope.go` — `envelope` struct, `envelopeRevision` constant with SYNC block, `envelopeFromOptions(opts)`, `ComputeEnvelopeFormat(env)`. Reads live values for thresholds, gzip levels, incompressible-extension list, and media types so parameter changes auto-propagate to the digest.
- `pkg/model/envelope_test.go` — snapshot tests with a long-form `envelopeFormatChangeMessage` constant that explains the four resolution cases (intentional parameter change / byte-level change without parameter change / revert / unintentional struct edit). Also tests determinism, revision-bump-changes-digest, and per-field coverage.
- `integration-tests/tests/weights_import_predict.txtar` — verifies `cog weights import` then `cog predict` (no pull) end-to-end.

**Lockfile**
- `pkg/weights/lockfile/lockfile.go` — added top-level `EnvelopeFormat string` field. Always stamped on rewrite; missing/empty parses cleanly and forces recompute on next import.

**Packer**
- `pkg/model/packer.go` — restructured around streaming-from-store. Drops on-disk tar scratch entirely. New `computeLayerDigests(ctx, store, plan)` streams each layer through tar+gzip+sha256+counter to `io.Discard`. New `ingressFromInventory(ctx, src, store, inv)` populates the store hash-verified, skipping already-present digests. Tar member bytes come from `store.Path(digest)`.
- `gzipLevelBundle` and `gzipLevelLarge` extracted as named constants so the envelope picks them up.
- `packedLayer` no longer carries `TarPath`. It carries the `layerPlan` so push can replay the same packing pipeline against the warm store.

**Builder**
- `pkg/model/weight_builder.go` — rewritten around the new flow:
  1. Always inventory + ingress (warms the store).
  2. Compare current envelope to lockfile's `EnvelopeFormat`; if matched and the user-intent fields and inventory match the locked entry, take the fast path and trust recorded layer digests (paired with replanned `layerPlan`s for push).
  3. Otherwise recompute layer digests by streaming from the store.
  4. Always stamp the current `EnvelopeFormat`. Rewrite the lockfile only if the entry or the stamp actually changed.
- `WeightArtifact` now carries the `store.Store` so push can stream layer bytes.
- `fileLayer` rewritten: takes `(packedLayer, store)`, reproduces tar bytes via `io.Pipe` + `streamLayer` on each `Compressed()` call. Deterministic for a given (plan, store) pair.

**Wiring**
- `pkg/cli/weights.go`: constructs a `FileStore` from `paths.WeightsStoreDir()` and threads it into `NewWeightBuilder`.
- `pkg/model/resolver.go`: same, plus a tiny `openWeightsStore()` helper for shared ownership.

**Removed**
- `WeightsCacheDir` constant, `.cog/weights-cache/` machinery, `cacheDirFor`, `resetCacheDir`, `cachedLayers`, `layerCachePath`, `digestToFilename`, `cleanupPackedLayers` — all gone.
- `packer.pack()` and `packer.execute()` — replaced by the planning/streaming split.
- `packOptions.TempDir` — no longer needed.

**New tests**
- `TestWeightBuilder_PopulatesStore` — Build warms store with imported files.
- `TestWeightBuilder_FastPath_PopulatesEmptyStore` — fresh-clone scenario: lockfile present but cold store, fast path still ingresses.
- `TestWeightBuilder_FastPath_StoreWarm_NoSourceReadForKnownFiles` — second build with unchanged source is a no-op on the lockfile.
- `TestWeightBuilder_DigestMismatch_FailsLoudly` — `store.PutFile` rejects mismatched digests.
- `TestWeightBuilder_EnvelopeFormatMismatch_TriggersRecompute` — corrupt lockfile envelope forces recompute and is restamped.
- `TestWeightBuilder_StampsEnvelopeFormat` — every successful Build stamps the current envelope.
- `TestFileLayer_ReturnsLayerBytes` — replaces the old TarPath-based test; verifies streamed bytes hash to the recorded layer digest.
- `TestFileLayer_NilStoreFailsClosed` — explicit failure mode for misuse.

**Risks flagged in design (unchanged)**
- EXDEV between cache and project dir on `cog predict` — pre-existing, doc note.
- Source drift now surfaces loudly during ingress (improvement).
- `envelopeFormat` lockfile churn across cog versions — same dynamic as `package-lock.json` across node versions.

`mise run lint:go`: 0 issues. `go test ./...`: all passing. `go test -tags integration -count=0 ./integration-tests/`: compiles.
