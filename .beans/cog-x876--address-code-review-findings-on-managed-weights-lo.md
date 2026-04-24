---
# cog-x876
title: Address code review findings on managed-weights local-run
status: completed
type: task
priority: high
created_at: 2026-04-24T14:35:28Z
updated_at: 2026-04-24T14:48:13Z
---

Three parallel review agents (safety/error handling, style/design, testing/perf) flagged issues on the code review pass. Triaged into:

**Critical:**
- Mount leak in Predictor.Start when Prepare succeeds but RunDaemon/port lookup fails. Callers' defer Stop is only registered after Start success, so failure path orphans .cog/mounts/<id>/.

**Important:**
- PullEventKind zero value = PullEventWeightStart (real event) — add PullEventUnknown sentinel
- Variable shadowing of err in Predictor.Start (mounts, err := shadows var err error)
- newWeightManager business logic lives in pkg/cli/ but belongs in pkg/weights/ — move it
- --image flag uses GetString with silently-dropped error — switch to StringVar like --verbose
- Path-traversal defense in assembleWeightDir: validate entry.Name and f.Path don't escape
- Manager.registry field types against fat registry.Client — narrow to imageFetcher interface

**Minor:**
- PullEvent field grouping comments (cluster by Kind)
- store.File → store.FileStore rename (FileStore is clearer than File given os.File exists)
- paths.WeightsStoreDir() error silently dropped in verbose block
- IT regex [0-9a-f]{12} pinning — relax to [0-9a-f]+
- Add t.Parallel() to independent unit tests
- Add test for ctxReader context cancellation mid-PutFile

**Pushed back:**
- NewPredictor unused ctx, pullLayer 7 params, PullEvent sum-type, CodedError, table-driven for degenerate case, MkdirAll dedup micro-opt, existsHex refactor

## Summary of Changes

### Critical
- **Mount leak on Predictor.Start failure** (pkg/predict/predictor.go): Start now uses a named return + defer to Release() mounts on any error path after Prepare succeeds. Regression test in pkg/predict/predictor_test.go mocks ContainerStart to fail and asserts .cog/mounts/ is cleaned up. Verified the test fails without the fix and passes with it.

### Important
- **PullEventUnknown sentinel** (pkg/weights/pull.go): added iota 0 so a zero-valued PullEvent is distinguishable from a legitimate WeightStart event.
- **Variable shadowing in Start** (pkg/predict/predictor.go): dropped the outer var err error; containerID now uses := in its own line.
- **newWeightManager moved to pkg/weights/** as weights.NewFromSource (pkg/weights/setup.go). The CLI helper in pkg/cli/weights_manager.go shrank to just the repo-parsing shell (parseRepoOnly stays in CLI as input-parsing concern).
- **--image flag uses StringVar** for consistency with --verbose (pkg/cli/weights_pull.go).
- **Path-traversal defense** via safeJoin in pkg/weights/mount.go: validates entry.Name and f.Path don't escape the mount root. TestSafeJoin + TestPrepare_RejectsPathTraversalInWeightName + TestPrepare_RejectsPathTraversalInFilePath cover the behavior.
- **Manager.registry narrowed to imageFetcher interface** (pkg/weights/manager.go): declares the one method Manager actually calls (GetImage). Makes the dependency explicit and simplifies future mocks. ManagerOptions.Registry keeps the wider registry.Client for caller convenience.

### Minor
- **PullEvent field grouping** (pkg/weights/pull.go): fields now clustered by which Kind populates them, with doc comments.
- **store.File → store.FileStore** rename: FileStore is clearer than File given os.File exists in the same mental namespace. The test helper variable `fs` is less confusing now too (it's a file store, not a file).
- **paths.WeightsStoreDir() in verbose block** (pkg/cli/weights_pull.go): explicit nolint comment explaining the error is unreachable here (newWeightManager would have failed earlier).
- **IT regex [0-9a-f]{12} → [0-9a-f]+** (integration-tests/tests/weights_pull.txtar): decoupled from ShortDigest's length.
- **t.Parallel() on independent unit tests** in pkg/weights/store/file_test.go, pkg/weights/pull_test.go, pkg/weights/mount_test.go. Race-clean.
- **TestFile_PutFile_ContextCanceled**: new test for ctxReader cancellation via a gatedReader that cancels after the first byte. Deterministic (not timing-based). Ran 20x, no flakes.

### Pushed back (from the review)
- NewPredictor unused ctx: out of scope, pre-existing
- pullLayer 7 params: reviewer themselves landed on 'leave it' after analysis
- PullEvent sum-type via interfaces: reviewer concluded current shape is right
- CodedError usage: not established codebase convention
- MkdirAll per-parent-dir dedup: micro-opt

`mise run lint:go` → 0 issues. `go test ./...` → all green. Integration tests pass. New predict regression test verified to catch the mount-leak bug when fix is reverted.
