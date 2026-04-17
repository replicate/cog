---
# cog.md-managed-weights-v0.0.2-6wm0
title: Include/exclude filters for weight import
status: todo
type: task
priority: normal
created_at: 2026-04-17T19:27:37Z
updated_at: 2026-04-17T19:27:37Z
parent: cog.md-managed-weights-v0.0.2-9qcd
blocked_by:
    - cog.md-managed-weights-v0.0.2-2gv9
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
