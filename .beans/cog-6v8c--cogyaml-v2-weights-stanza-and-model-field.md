---
# cog-6v8c
title: cog.yaml v2 weights stanza and model field
status: in-progress
type: task
priority: critical
created_at: 2026-04-17T19:27:46Z
updated_at: 2026-04-21T20:57:52Z
parent: cog-66gt
---

Full implementation of the v2 cog.yaml weights configuration.

Changes from v0:
- weights entries are directory-based, not file-based
- source is a nested object with uri, include, exclude (not a flat string)
- New top-level model field for registry namespace
- Derived paths: <model>/weights/<name> for weight repositories
- Update JSON schema (pkg/config/data/config_schema_v1.0.json)
- Update config parsing (pkg/config/config.go, parse.go, config_file.go)
- Validate target directory constraints (unique, disjoint subtrees)
- Error if weights stanza present without model field

The initial e2e import task does minimal parsing. This task completes it with full validation, schema updates, and the model field.

Reference: plans/2026-04-16-managed-weights-v2-design.md §2

## Implementation Plan

- [x] Add `Model` field to `configFile` and `Config` types
- [x] Restructure `WeightSource.Source` from flat string to nested `WeightSourceConfig` object (uri, include, exclude)
- [x] Update `weightFile` / parsing to handle the new nested source shape
- [x] Update JSON schema: add `model`, restructure `source` as object, change required fields (name+target required, source optional)
- [x] Add `validateWeights` with: name required+unique, target required+absolute+unique, disjoint subtrees, model required when weights present
- [x] Wire `validateWeights` into `ValidateConfigFile`
- [x] Update existing weight tests and add validation tests
- [x] Run `mise run lint:go` and `mise run test:go` to verify

- [x] Add weight name format validation (OCI tag-safe characters)
