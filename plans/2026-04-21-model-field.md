# `model` field in cog.yaml

## Problem

`image` currently does two jobs: it names the local Docker image (`cog build -t`) and serves as the registry push target (`cog push`). These are conceptually different things, and they diverge once managed weights enter the picture -- weights need a stable registry namespace to derive `<model>/weights/<name>` paths, but the local Docker tag is a development convenience.

Even without weights, decoupling "where I push" from "what I tag locally" is useful: you might want `cog build -t my-model:dev` for local iteration while always pushing to `registry.example.com/team/my-model`.

## What `model` does

`model` is a top-level cog.yaml field that establishes the registry namespace.

```yaml
model: registry.example.com/team/my-model
```

It controls:
- **`cog push` destination** -- pushes to `<model>` instead of `<image>`
- **Weight repository paths** -- derived as `<model>/weights/<name>`
- **Bundle references** -- `<model>:latest`, `<model>:v1`
- **Local Docker tag derivation** -- when `image` is omitted, `cog build` tags as `<model-basename>:latest`

`image` remains the explicit local Docker tag override. When both are set, `image` controls local naming, `model` controls the registry.

## Override hierarchy

Highest wins:

1. CLI argument: `cog push registry.staging.example.com/team/my-model`
2. Environment variable: `COG_MODEL`
3. cog.yaml `model` field

This handles multi-environment workflows (prod, staging, dev registries) without per-environment config files.

## Behavior matrix

| cog.yaml has | `cog build` tags as | `cog push` pushes to |
|---|---|---|
| `image` only | `<image>` | `<image>` (today's behavior, unchanged) |
| `model` only | `<basename>:latest` | `<model>` |
| `model` + `image` | `<image>` | `<model>` |
| neither | `cog-<dirname>:latest` | error: no push target |

When `weights` is present, `model` is required. `image` + `weights` without `model` is an error (weights need a registry namespace to derive paths).

## Current state

The `model` field is already parsed and stored in `Config.Model` (added in the weights stanza PR). The JSON schema accepts it. Validation enforces "model required when weights present." But **nothing consumes it** -- no CLI command reads `Config.Model` for routing, tagging, or pushing.

## What needs to happen

### 1. `cog push` reads `model` for the push target

`pkg/cli/push.go:69` currently does:
```go
imageName := src.Config.Image
```

This needs to become a resolution chain: CLI arg > `Config.Model` > `Config.Image`. The resolved name is the push destination.

### 2. `cog build` derives local tag from `model` when `image` is absent

`pkg/cli/build.go:86` currently falls back to `DockerImageName(projectDir)` when `image` is empty. When `model` is set, it should derive a local tag from the model basename instead.

### 3. `cog weights push` uses `model` as the repository base

`pkg/cli/weights.go:149` uses `cfg.Image` as the repository base for weight push. This should prefer `Config.Model`.

### 4. `COG_MODEL` environment variable

Read `COG_MODEL` at config resolution time. This slots between CLI args and the cog.yaml field in the override hierarchy. Implementation: either mutate `Config.Model` after loading, or resolve at each usage point.

### 5. CLI flag (optional, lower priority)

A `--model` persistent flag on root would provide the CLI-level override. Stored in `pkg/global` like `--registry`. Lower priority than the env var since most multi-environment workflows use env vars.

## Impact on existing commands

Commands that currently reference `Config.Image`:

| Command | File | Current behavior | Change needed |
|---|---|---|---|
| `cog build` | `pkg/cli/build.go:86` | Uses `image` or `-t` flag for local tag | Fall back to model basename when image absent |
| `cog push` | `pkg/cli/push.go:69` | Uses `image` or positional arg as push target | Prefer `model` over `image` for push target |
| `cog weights push` | `pkg/cli/weights.go:149` | Uses `image` as repo base | Prefer `model` |
| Provider PostPush | `pkg/provider/replicate/replicate.go:88` | Uses image name in success message | Use resolved push target (model or image) |

Commands that don't need changes: `cog predict`, `cog train`, `cog run` (local execution, don't care about registry namespace).

## Non-goals for the initial PR

- Config file layering (`cog.yaml` + `cog.prod.yaml`)
- Model field format validation (e.g. must be `host/owner/name`)
- Tag/version subfields on model
- Interaction with `COG_OCI_INDEX` (already removed per design doc)
