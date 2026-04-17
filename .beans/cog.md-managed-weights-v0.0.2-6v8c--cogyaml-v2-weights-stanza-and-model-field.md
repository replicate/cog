---
# cog.md-managed-weights-v0.0.2-6v8c
title: cog.yaml v2 weights stanza and model field
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:27:46Z
updated_at: 2026-04-17T19:27:46Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
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
