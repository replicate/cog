---
# cog-p6oj
title: Remove weights-gen and unexport dead symbols
status: in-progress
type: task
created_at: 2026-04-23T04:54:27Z
updated_at: 2026-04-23T04:54:27Z
---

Remove tools/weights-gen (matches PR #2959), then unexport ~22 symbols in pkg/model/ and pkg/model/weightsource/ that were only exported for that tool.
