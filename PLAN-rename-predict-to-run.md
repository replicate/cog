# Plan: Rename "predict" to "run"

Rename the core "predict" concept to "run" across the CLI, Python SDK, HTTP API,
and internals. Maintain full backwards compatibility — old names continue to work
as hidden aliases.

## Summary of changes

| Current | New | Backwards compat |
|---------|-----|-----------------|
| `cog predict [image]` | `cog run [image]` | `cog predict` kept as hidden alias |
| `cog run <cmd>` | `cog exec <cmd>` | Old `cog run <cmd>` usage detected, prints migration hint |
| `predict:` in cog.yaml | `run:` in cog.yaml | `predict:` still accepted |
| `class BasePredictor` | `class Runner` | `BasePredictor` kept as alias |
| `def predict(self, ...)` | `def run(self, ...)` | `predict()` method still detected |
| `predict.py` (convention) | `run.py` (convention) | Old filenames still work |
| `POST /predictions` | `POST /runs` | `/predictions` kept |
| `PUT /predictions/{id}` | `PUT /runs/{id}` | `/predictions/{id}` kept |
| `POST /predictions/{id}/cancel` | `POST /runs/{id}/cancel` | `/predictions/{id}/cancel` kept |
| `predict_time` metric | `run_time` metric | `predict_time` still emitted alongside |

## Phase 1: CLI commands

### 1a. Rename `cog run` → `cog exec`

- Rename `pkg/cli/run.go` → `pkg/cli/exec.go`
- Rename `newRunCommand()` → `newExecCommand()`, `run()` → `execCmd()`
- Change `Use: "exec <command> [arg...]"`
- Register as `cog exec` in `root.go`

### 1b. New `cog run` = prediction command

- Create the new `cog run` command reusing `cmdPredict` logic
- `Use: "run [image]"`, same flags as current `cog predict`
- Add heuristic detection of old `cog run` usage:
  - >1 positional args → error with migration message
  - 1 arg + no `-i`/`--json` flags + arg has no `/` with `.` prefix, no `:`, no `@sha256:` → error with migration message
- Migration message: `"cog run" now runs predictions. Use "cog exec" to run commands.\n\n  cog exec <cmd>`

### 1c. `cog predict` as hidden alias

- Keep `newPredictCommand()` but set `Hidden: true`
- Points to the same `RunE` handler

## Phase 2: cog.yaml configuration

- Add `Run string` field to `Config` struct in `pkg/config/config.go`
- Accept both `run:` and `predict:` keys
- If both are set, return a validation error
- Resolve: if `run:` is set, use it; else fall back to `predict:`
- Update JSON schema `pkg/config/data/config_schema_v1.0.json` to include `run`
- Update `cog init` template to generate `run: "run.py:Runner"`

## Phase 3: Python SDK

- Add `Runner` class in `python/cog/` (new name for `BasePredictor`)
- `BasePredictor` becomes an alias for `Runner`
- Support `def run(self, ...)` as the primary method name
- Detection: if class has `run()`, use it. If not, fall back to `predict()`. If both, error.
- Export `Runner` from `python/cog/__init__.py`
- Update `cog init` to generate `run.py` with `Runner` class and `def run()` method

## Phase 4: HTTP API

- Add `/runs`, `/runs/{id}`, `/runs/{id}/cancel` routes in Rust coglet
  (`crates/coglet/src/transport/http/routes.rs`)
- These are aliases for the existing `/predictions` handlers
- Keep `/predictions` routes working
- Add `run_time` to metrics output alongside `predict_time`

## Phase 5: Go internals

- Rename `pkg/predict/` → `pkg/run/`
- Rename `predict.Predictor` → `run.Runner`
- Rename `predictor.Predict()` → `runner.Run()`
- Update all Go imports

## Phase 6: Rust internals

- Add `run_time` metric alongside `predict_time` in prediction metrics
- Route aliases only (no deep type renames yet — that's a future cleanup)

## Phase 7: Docs and tests

- Update all docs to use `cog run` as the primary command
- Update `cog init` template
- Update integration tests that use `cog predict` → `cog run`
- Add new integration tests for `cog exec`
- Regenerate `docs/llms.txt` and `docs/cli.md`
