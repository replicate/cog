---
# cog-vovc
title: Remove redundant model field from cog.yaml config
status: todo
type: task
priority: normal
created_at: 2026-04-24T06:05:17Z
updated_at: 2026-04-24T06:05:26Z
---

The `model` config field is redundant with `image` and should be removed.

## Context

The `model` field was intended as a registry namespace for deriving image and weight paths, but in practice it is never consumed by any CLI command or business logic. The `image` field does all the actual work — build, push, weight import/pull/status all read `Config.Image`.

Today `model` is:
- Parsed from YAML and stored in the config struct, but never read by any command
- Only referenced in a validation rule requiring it when `weights` are configured (`validate.go:472-476`)
- Set to the same value as `image` in the managed-weights example

## Work

- [ ] Remove `model` from the JSON schema (`pkg/config/data/config_schema_v1.0.json`)
- [ ] Remove `Model` from Go config structs (`config.go`, `config_file.go`)
- [ ] Remove `model` parsing in `parse.go`
- [ ] Update validation in `validate.go` — the weights validation should check `image` instead of `model`
- [ ] Update/remove tests in `config_test.go` and `validate_test.go`
- [ ] Remove `model` from the managed-weights example (`examples/managed-weights/cog.yaml`)
- [ ] Update docs (`docs/yaml.md`) — remove any `model` references
- [ ] Regenerate `docs/llms.txt` via `mise run docs:llm`
- [ ] Consider deprecation path: should we warn on `model` before hard-removing it?
