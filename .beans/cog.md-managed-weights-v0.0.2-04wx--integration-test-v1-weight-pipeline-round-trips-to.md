---
# cog.md-managed-weights-v0.0.2-04wx
title: 'Integration test: v1 weight pipeline round-trips to correct on-disk shape'
status: completed
type: task
priority: high
created_at: 2026-04-18T00:36:04Z
updated_at: 2026-04-18T03:07:20Z
parent: cog.md-managed-weights-v0.0.2-9qcd
---

We have no test that verifies the end-to-end v1 weight pipeline produces extractable artifacts: `WeightBuilder.Build` → `WeightPusher.Push` → `crane pull` → extract → compare to source. The `oci_bundle_push.txtar` integration test exists but still asserts the stale v0 `vnd.cog.*` annotations and uses file-source (v0 shape) rather than directory-source (v1).

## Plan
- Add Go test `TestWeightPipeline_EndToEnd` in `pkg/model/` that:
  - Uses `registry_testhelpers.StartTestRegistry` for a real local registry
  - Generates a source dir with all layer types: bundled small files, a single-file incompressible (`.safetensors`), a single-file compressible (`.nemo`), via `PackOptions` tuned so thresholds are crossed with small payloads
  - Runs `WeightBuilder.Build` + `WeightPusher.Push`
  - Pulls the manifest back with `crane` and asserts spec-compliant annotations
  - Pulls each layer blob back, extracts the tar, walks the tree, and asserts every file path + size + sha256 content matches the source
- Fix `integration-tests/tests/oci_bundle_push.txtar`:
  - Switch to directory-source (`source: weights`) with multi-file content
  - Update annotation assertions from `vnd.cog.*` to `run.cog.*`
  - Assert the pushed ref is an OCI index with one image + one weight manifest
  - Keep test fast (tiny payloads)
- Optionally: add a harness command `registry-extract-layer` if we want on-disk shape assertions at the testscript level — deferred for now.

## Acceptance
- `go test ./pkg/model/ -run TestWeightPipeline_EndToEnd` passes, takes <30s
- `mise run test:integration oci_bundle_push` passes against v1 pipeline
- If a regression is introduced (e.g. layer annotation key changes, or tar-header ordering breaks extraction), at least one test fails



## Summary of Changes

- **pkg/model/weight_pipeline_e2e_test.go** (new, ~230 lines): `TestWeightPipeline_EndToEnd` packs a source dir with bundle/small/incompressible/compressible files, pushes via `WeightPusher` to a `registry_testhelpers.StartTestRegistry` container, pulls each layer blob by digest, extracts the tar, and asserts every source file is present byte-identical (checked via sha256). Also verifies manifest-level annotations (`run.cog.*`), `artifactType: application/vnd.cog.weight.v1`, media-type choices for incompressible vs compressible files, and the `run.cog.weight.size.uncompressed` annotation.

- **integration-tests/tests/oci_bundle_push.txtar** (rewritten): was asserting stale v0 `vnd.cog.*` annotations with file-source (v0 shape). Switched to directory-source (v1), updated annotations to `run.cog.*`, and split the single `Pushing` stderr check into distinct `Pushing image` and `Pushing weights...` checks now that the CLI prints a weight-phase header (landed separately as bean `seb1`).

Runtime: Go e2e test ~7.5s cold-start / ~1.3s with cached container. Integration test ~19s.

Skipped in short mode (both tests). Runs in CI via `mise run test:integration` and the standard go test invocation (not under `-short`).
