---
# cog-ng36
title: Weight caching and build-context exclusion
status: todo
type: task
priority: low
created_at: 2026-04-17T23:12:10Z
updated_at: 2026-04-22T17:32:19Z
parent: cog-66gt
---

Two related problems around weight data during builds:

## 1. Build-context bloat (local file:// sources only)

When `cog.yaml` has managed `weights:` entries with local `file://` sources that happen to be inside the Docker build context, `COPY . /src` drags multi-GB weight dirs into buildkit for no reason — weights arrive at runtime via OCI layer extraction, not at build time.

This is worth fixing but **only applies when the source path is within the docker context**. A `.dockerignore` exclusion is the right fix for this narrow case.

## 2. Weight caching/buffering during import (the bigger problem)

The more important question is how we cache and buffer weight data while working on them locally. Consider:

- Source on NFS or S3: `.dockerignore` is useless — the data isn't in the context
- Iterating on weights locally: re-packing and re-uploading multi-GB on every import is painful
- The `.cog/weights-cache/` packed-tar cache helps for re-push but doesn't address fetch/download caching

We need a local weight cache strategy that works regardless of source scheme:
- Local cache directory for fetched/packed weight data
- Content-addressed so repeated imports of unchanged weights are fast
- Works with file://, hf://, s3://, http:// sources
- Cache invalidation tied to source fingerprinting (cog-s5fy)

## Original .dockerignore repro (for reference)

```bash
cd examples/test-weights
rm .dockerignore
COG_OCI_INDEX=1 cog push localhost:5000/test-weights
# transferring context: 9.87GB 64.4s (without ignore)
# transferring context: 11.97 MB 0.1s (with ignore)
```

## Tasks
- [ ] For local sources within docker context: auto-exclude `cfg.Weights[].Source` paths from `.dockerignore`
- [ ] Also exclude `.cog/weights-cache/` but NOT `.cog/tmp/`
- [ ] Design local weight cache strategy that works across source schemes
- [ ] Content-addressed cache so unchanged weights skip re-pack/re-upload
- [ ] Tie cache invalidation to source fingerprinting (cog-s5fy)

## Design note: lockfile vs working directory (from 6b5a discussion, 2026-04-22)

The lockfile (`weights.lock`) should only contain fully resolved, complete state — it's a commit artifact. Intermediate/in-flight state belongs in a separate working directory (e.g. `.cog/weights/`), not git-tracked.

Model:
- `weights.lock` — clean, complete, git-tracked. Only written when an import fully succeeds for a given weight.
- `.cog/weights/<name>/` — working directory for in-flight state. Partial downloads, packed layers that haven't been pushed, progress tracking. In `.gitignore`.

Flow:
1. `cog weights import` starts processing, writes progress to `.cog/weights/<name>/`
2. If interrupted, `.cog/weights/<name>/` has partial state but `weights.lock` is untouched (or has the previous successful entry)
3. Resume: import picks up from working dir, finishes, then atomically updates `weights.lock`
4. The lockfile is never in a half-baked state

This separation means `cog weights status` can cleanly determine state: lockfile entry present = build completed at some point. Registry check = push completed. Working dir state = in-flight (future scope for status).

The existing `.cog/weights-cache/` packed-tar cache is a precursor to this pattern. This bean should formalize the working directory layout when implementing the cache strategy.
