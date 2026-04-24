---
# cog-vovc
title: Remove redundant model field from cog.yaml config
status: in-progress
type: task
priority: normal
created_at: 2026-04-24T06:05:17Z
updated_at: 2026-04-24T20:15:16Z
---

The `model` config field is redundant with `image` and should be removed.

## Context

The `model` field was intended as a registry namespace for deriving image and weight paths, but in practice it is never consumed by any CLI command or business logic. The `image` field does all the actual work — build, push, weight import/pull/status all read `Config.Image`.

Today `model` is:
- Parsed from YAML and stored in the config struct, but never read by any command
- Only referenced in a validation rule requiring it when `weights` are configured (`validate.go:472-476`)
- Set to the same value as `image` in the managed-weights example

## Work

- [x] Remove `model` from the JSON schema (`pkg/config/data/config_schema_v1.0.json`)
- [x] Remove `Model` from Go config structs (`config.go`, `config_file.go`)
- [x] Remove `model` parsing in `parse.go`
- [x] Update validation in `validate.go` — weights now require `image` instead of `model`
- [x] Update/remove tests in `config_test.go` and `validate_test.go`
- [x] Remove `model` from the managed-weights example (`examples/managed-weights/cog.yaml`)
- [x] Update docs (`docs/yaml.md`) — no `model` field was ever documented there; nothing to remove
- [x] Regenerate `docs/llms.txt` via `mise run docs:llm`
- [x] Deprecation path considered — hard removal chosen: the field is only days old (from 2026-04-21 managed-weights work), only appears in a dev example and two integration fixtures, and `additionalProperties: false` in the schema makes any keep-and-warn strategy awkward

## Summary of Changes

Removed the `model` field from cog.yaml configuration entirely:

- **Schema** (`pkg/config/data/config_schema_v1.0.json`): dropped the `model` property; updated the `<model>/weights/<name>` comment to `<image>/weights/<name>`.
- **Structs** (`config.go`, `config_file.go`): removed the `Model` field from both `Config` and `configFile`.
- **Parsing** (`parse.go`): removed the `cfg.Model` copy step.
- **Validation** (`validate.go`): the "weights require a model" rule now reads "weights require an image," checking `cfg.Image` instead.
- **Tests** (`config_test.go`, `validate_test.go`): fixtures switched from `model:` to `image:`; the renamed test case is "missing image" with the corresponding error message.
- **Example** (`examples/managed-weights/cog.yaml`): dropped the redundant `model:` line (it was a duplicate of `image:`).
- **Integration fixtures** (`integration-tests/tests/weights_pull.txtar`, `weights_pull_predict.txtar`): `model:` → `image:` so they still satisfy the new validation rule.
- **Plan file**: deleted `plans/2026-04-21-model-field.md` — it described wiring `model` up as a separate namespace, which is no longer the direction.
- **Docs**: regenerated `docs/llms.txt`. `docs/yaml.md` never documented the field, so no content edits needed.

Verified:
- `go build ./...` clean
- `mise run test:go` — 1486 tests pass, 5 skipped (unrelated integration/docker skips)
- `mise run lint:go` — 0 issues

Follow-up not done (out of scope, flagging for later):
- `plans/2026-04-16-managed-weights-v2-design.md` still references the `model` field in section 2. That's a broader managed-weights v2 design doc; editing it crosses into revising architectural history rather than removing this field, so I left it untouched.
