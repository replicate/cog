# Makefile - Shim that delegates to mise tasks
#
# This Makefile provides backward compatibility for common targets.
# All task definitions live in mise.toml. Run `mise tasks` to see available tasks.
#
# For new development, prefer using mise directly:
#   mise run build:cog    instead of    make cog
#   mise run test:go      instead of    make test-go
#   mise run fmt:fix      instead of    make fmt

SHELL := bash

DESTDIR ?=
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

INSTALL := install -m 0755

GO ?= go

COG_BINARIES := cog base-image

default: cog

# =============================================================================
# Build targets
# =============================================================================

.PHONY: cog base-image
$(COG_BINARIES):
	mise run build:cog

.PHONY: wheel
wheel:
	mise run build:sdk

.PHONY: install
install: cog
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) cog $(DESTDIR)$(BINDIR)/cog

# =============================================================================
# Test targets
# =============================================================================

.PHONY: test
test:
	mise run test:go
	mise run test:python

.PHONY: test-go
test-go:
	mise run test:go

.PHONY: test-python
test-python:
	mise run test:python

.PHONY: test-integration
test-integration:
	mise run test:integration

.PHONY: test-coglet
test-coglet: test-coglet-rust

.PHONY: test-coglet-rust
test-coglet-rust:
	mise run test:rust

.PHONY: test-coglet-python
test-coglet-python:
	mise run test:coglet:python

# =============================================================================
# Format and lint targets
# =============================================================================

.PHONY: fmt
fmt:
	mise run fmt:fix

.PHONY: check-fmt
check-fmt:
	mise run fmt

.PHONY: lint
lint:
	mise run lint

.PHONY: vet
vet:
	$(GO) vet ./...

# =============================================================================
# Code generation
# =============================================================================

.PHONY: generate
generate:
	mise run generate:go

.PHONY: gen-mocks
gen-mocks:
	mockery

# =============================================================================
# Coglet (Rust) targets
# =============================================================================

.PHONY: fmt-coglet
fmt-coglet:
	mise run fmt:rust:fix

.PHONY: check-fmt-coglet
check-fmt-coglet:
	mise run fmt:rust

.PHONY: lint-coglet
lint-coglet:
	mise run lint:rust

# =============================================================================
# Documentation
# =============================================================================

.PHONY: run-docs-server
run-docs-server:
	mise run docs:serve

# =============================================================================
# Clean
# =============================================================================

.PHONY: clean
clean:
	mise run clean

.PHONY: clean-coglet
clean-coglet:
	mise run clean:rust
