SHELL := bash

DESTDIR ?=
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

INSTALL := install -m 0755

GO ?= go
# GORELEASER := $(GO) tool goreleaser
GORELEASER := $(GO) run github.com/goreleaser/goreleaser/v2@latest
GOIMPORTS := $(GO) tool goimports
GOLINT := $(GO) tool golangci-lint

UV ?= uv
TOX := $(UV) run tox

COG_GO_SOURCE := $(shell find cmd pkg -type f -name '*.go')

COG_BINARIES := cog base-image

default: all

.PHONY: all
all: cog

$(COG_BINARIES): $(COG_GO_SOURCE) generate
	@echo Building $@
	@if git name-rev --name-only --tags HEAD | grep -qFx undefined; then \
		GOFLAGS=-buildvcs=false $(GORELEASER) build --clean --snapshot --single-target --id $@ --output $@; \
	else \
		GOFLAGS=-buildvcs=false $(GORELEASER) build --clean --auto-snapshot --single-target --id $@ --output $@; \
	fi

.PHONY: install
install: $(COG_BINARIES)
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) $< $(DESTDIR)$(BINDIR)/$<

.PHONY: wheel
wheel:
	script/build-wheels

.PHONY: clean
clean: clean-coglet
	rm -rf .tox build dist pkg/wheels/*.whl
	rm -f $(COG_BINARIES)

.PHONY: test-go
test-go: generate
	$(GO) tool gotestsum -- -short -timeout 1200s -parallel 5 $$(go list ./...) $(ARGS)

# Run Go-based integration tests (testscript)
# Use TEST_PARALLEL to control concurrency (default 4 to avoid Docker overload)
# CI with more cores can set TEST_PARALLEL=8 or higher
TEST_PARALLEL ?= 4
.PHONY: test-integration
test-integration: $(COG_BINARIES)
	$(GO) test ./pkg/docker/...
	cd integration-tests && $(GO) test -v -parallel $(TEST_PARALLEL) -timeout 30m $(ARGS) ./...

.PHONY: test-python
test-python: generate
	$(TOX) run --installpkg $$(ls dist/cog-*.whl) -f tests

.PHONY: test
test: test-go test-python

.PHONY: fmt
fmt:
	$(GOIMPORTS) -w -d .
	uv run ruff format
	cd crates && cargo fmt

.PHONY: generate
generate:
	$(GO) generate ./...

.PHONY: vet
vet: generate
	$(GO) vet ./...

.PHONY: check-fmt
check-fmt:
	$(GOIMPORTS) -d .
	@test -z $$($(GOIMPORTS) -l .)

.PHONY: lint
lint: generate check-fmt vet
	$(GOLINT) run ./...
	$(TOX) run --installpkg $$(ls dist/cog-*.whl) -e lint,typecheck-pydantic2
	cd crates && cargo clippy -- -D warnings

.PHONY: run-docs-server
run-docs-server:
	uv pip install mkdocs-material
	sed 's/docs\///g' README.md > ./docs/README.md
	cp CONTRIBUTING.md ./docs/
	mkdocs serve

.PHONY: gen-mocks
gen-mocks:
	mockery

# =============================================================================
# Coglet targets (Rust)
# =============================================================================

# Run coglet Rust tests
.PHONY: test-coglet-rust
test-coglet-rust:
	cd crates && cargo test $(ARGS)

# Run coglet Rust linter
.PHONY: lint-coglet
lint-coglet:
	cd crates && cargo clippy -- -D warnings

# Format coglet Rust code
.PHONY: fmt-coglet
fmt-coglet:
	cd crates && cargo fmt

# Check coglet Rust formatting
.PHONY: check-fmt-coglet
check-fmt-coglet:
	cd crates && cargo fmt --check

# Run all coglet tests
.PHONY: test-coglet
test-coglet: test-coglet-rust

# Clean coglet build artifacts
.PHONY: clean-coglet
clean-coglet:
	cd crates && cargo clean
