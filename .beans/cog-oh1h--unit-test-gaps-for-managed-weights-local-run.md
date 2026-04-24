---
# cog-oh1h
title: Unit test gaps for managed-weights local-run
status: completed
type: task
priority: normal
created_at: 2026-04-24T02:42:40Z
updated_at: 2026-04-24T02:44:33Z
---

Three missed-branch unit tests flagged during code review:

- `wrapLinkError` EXDEV path is 0% covered — this is the one user-facing error we hand-crafted for a real-world scenario (cache on a different filesystem than project). Refactors could silently lose the `COG_CACHE_DIR` hint and unit tests wouldn't notice. Replace `TestIsCrossDeviceLink` (tests the helper) with `TestWrapLinkError_EXDEV` (tests user-facing behavior).
- `Manager.Pull` mid-stream layer read error — we cover 'layer not in image', 'unexpected file in tar', and 'digest mismatch' but not 'layer's `Uncompressed()` reader errors halfway through the tar walk'. Real scenario: flaky network, truncated blob.
- `Manager.Pull` layer tar missing an expected file — lockfile claims file X is in layer L but the tar stream doesn't contain X. Should surface the 'missing expected file' error from the post-walk check in `pullLayer`.

Each is a small unit test using the existing in-memory fixtures (`buildWeightImage`, `rawTarLayer`, `stubRegistry`).

## Summary of Changes

Three unit tests added to `pkg/weights/`:

- `TestWrapLinkError_EXDEV`, `TestWrapLinkError_EXDEV_ThroughLinkError`, `TestWrapLinkError_NonEXDEV` (mount_test.go) — replaces the old internal-helper-focused `TestIsCrossDeviceLink`. Covers the bare `syscall.EXDEV`, the `*os.LinkError{Err: EXDEV}` chain (what `os.Link` actually returns), and a non-EXDEV error (to confirm the COG_CACHE_DIR hint is EXDEV-specific).
- `TestManager_Pull_LayerReadError` (pull_test.go) — new `truncatedLayer` test helper wraps `rawTarLayer` and returns a truncated reader via `io.MultiReader` + `errReader`. Asserts the Pull surfaces the underlying error and that the partially-received file is absent from the store (validates PutFile's temp+rename atomicity end-to-end).
- `TestManager_Pull_LayerMissingExpectedFile` (pull_test.go) — registry layer contains file A but lockfile claims both A and B live there. Asserts the post-walk `'missing expected file'` error fires and names the missing path.

Coverage: `wrapLinkError` 0% → 100%. `pullLayer` 83.3% → 86.1%. Package total: 83.5% → 85.0%.
