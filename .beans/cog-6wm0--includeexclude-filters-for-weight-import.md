---
# cog-6wm0
title: Include/exclude filters for weight import
status: todo
type: task
priority: low
created_at: 2026-04-17T19:27:37Z
updated_at: 2026-04-22T01:27:11Z
parent: cog-66gt
blocked_by:
    - cog-2gv9
    - cog-s5fy
---

Add include/exclude glob filtering to the weight import pipeline.

In cog.yaml:
  weights:
    - name: z-image-turbo
      target: /src/weights
      source:
        uri: hf://stabilityai/z-image-turbo
        exclude: ["*.onnx", "*.bin", "*.msgpack"]
        include: ["*.safetensors", "*.json"]

- exclude: glob patterns for files to skip during import
- include: glob patterns for allowlist mode (if set, only matching files are imported)
- Applied before tar packing (filter the file list, not the tars)
- Consistent with .gitignore-style glob semantics

This is deferred from the initial e2e import which only needs bare local directories.



## Dependency on s5fy (2026-04-21)

s5fy adds `include` and `exclude` fields to the lockfile's `source` block (`WeightLockSource` type). They are always serialized as `[]` when empty (never omitted), establishing the schema. However, s5fy does NOT apply filtering — it just records the patterns from cog.yaml into the lockfile.

This bean implements the actual filtering:
- Read patterns from `WeightSourceConfig` (cog.yaml)
- Pass them through the Source/builder pipeline
- Apply before tar packing: filter the walked file list using .gitignore-style glob semantics
- Record the applied patterns in `WeightLockSource.Include` / `WeightLockSource.Exclude`
- A pattern change triggers re-import even if the source fingerprint hasn't changed (different include/exclude = different effective file set)

Open question: whether we need both include AND exclude, or just exclude with gitignore-style negation patterns. Decide when implementing.
