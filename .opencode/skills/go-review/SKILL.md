---
name: go-review
description: Go code review guidelines for the Cog codebase
---

## Go review guidelines

This project uses Go for the CLI (`cmd/cog/`, `pkg/`) and support tooling (`tools/`).

### What linters already catch (skip these)

golangci-lint runs errcheck, gocritic, gosec, govet, ineffassign, misspell,
revive, staticcheck, and unused. Don't flag issues these would catch.

### What to look for

**Error handling**

- Errors returned but not checked or silently discarded
- User-facing errors should use `pkg/errors.CodedError` with error codes
- Generic error wrapping that loses context (`fmt.Errorf("failed")` with no `%w`)

**Imports**

- Should be three groups: stdlib, third-party, internal (`github.com/replicate/cog/pkg/...`)
- Only flag if actually wrong, not cosmetic reordering

**Testing**

- Must use `testify/require` for fatal assertions and `testify/assert` for non-fatal
- No raw `if` checks with `t.Fatal`/`t.Errorf`
- Prefer table-driven tests for similar cases
- Prefer specific assertions (`Equal`, `Contains`, `NoError`) over `True`/`False`

**Concurrency**

- Goroutine leaks (no cleanup path, missing context cancellation)
- Shared state without synchronization
- Channel misuse (sends on closed channels, unbuffered channels in wrong contexts)

**Docker/container patterns**

- The CLI uses the Docker Go SDK. Watch for leaked clients, unclosed response bodies
- Dockerfile generation is in `pkg/dockerfile/` -- template injection risks

**Architecture**

- Commands belong in `pkg/cli/`, business logic in `pkg/`
- Config parsing/validation in `pkg/config/`
- Don't mix CLI concerns with library logic
