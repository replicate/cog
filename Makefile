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

COG_GO_SOURCE := $(shell find cmd pkg -type f)
COG_PYTHON_SOURCE := $(shell find python/cog -type f -name '*.py')
COGLET_GO_SOURCE := $(shell find coglet -type f -name '*.go')
COGLET_PYTHON_SOURCE := $(shell find coglet/python -type f -name '*.py')

COG_BINARIES := cog base-image

default: all

.PHONY: all
all: cog

.PHONY: wheel
wheel: pkg/dockerfile/embed/.wheel

ifdef COG_WHEEL
pkg/dockerfile/embed/.wheel: $(COG_WHEEL)
	@mkdir -p pkg/dockerfile/embed
	@rm -f pkg/dockerfile/embed/*.whl # there can only be one embedded wheel
	@echo "Using prebuilt COG_WHEEL $<"
	cp $< pkg/dockerfile/embed/
	@touch $@
else
pkg/dockerfile/embed/.wheel: $(COG_PYTHON_SOURCE)
	@mkdir -p pkg/dockerfile/embed
	@rm -f pkg/dockerfile/embed/*.whl # there can only be one embedded wheel
	$(UV) build --wheel --out-dir=pkg/dockerfile/embed .
	@touch $@

define COG_WHEEL
    $(shell find pkg/dockerfile/embed -type f -name '*.whl')
endef

endif

$(COG_BINARIES): $(COG_GO_SOURCE) pkg/dockerfile/embed/.wheel
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

.PHONY: clean
clean: clean-coglet
	rm -rf .tox build dist pkg/dockerfile/embed
	rm -f $(COG_BINARIES)

.PHONY: test-go
test-go: pkg/dockerfile/embed/.wheel
	$(GO) tool gotestsum -- -short -timeout 1200s -parallel 5 $$(go list ./... | grep -v 'coglet/') $(ARGS)

.PHONY: test-integration
test-integration: $(COG_BINARIES)
	$(GO) test ./pkg/docker/...
	PATH="$(PWD):$(PATH)" $(TOX) -e integration

.PHONY: test-python
test-python: pkg/dockerfile/embed/.wheel
	$(TOX) run --installpkg $(COG_WHEEL) -f tests

.PHONY: test
test: test-go test-python test-integration

.PHONY: fmt
fmt:
	$(GOIMPORTS) -w -d .
	uv run ruff format

.PHONY: generate
generate:
	$(GO) generate ./...

.PHONY: vet
vet: pkg/dockerfile/embed/.wheel
	$(GO) vet ./...

.PHONY: check-fmt
check-fmt:
	$(GOIMPORTS) -d .
	@test -z $$($(GOIMPORTS) -l .)

.PHONY: lint
lint: pkg/dockerfile/embed/.wheel check-fmt vet
	$(GOLINT) run ./...
	$(TOX) run --installpkg $(COG_WHEEL) -e lint,typecheck-pydantic2

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
# Coglet targets
# =============================================================================

COGLET_BINARY_DIR := coglet/python/cog

# Build coglet-server binary for a specific OS/ARCH
# Usage: make coglet-server-binary GOOS=linux GOARCH=amd64
.PHONY: coglet-server-binary
coglet-server-binary: $(COGLET_GO_SOURCE)
	CGO_ENABLED=0 $(GO) build -o $(COGLET_BINARY_DIR)/cog-$(GOOS)-$(GOARCH) ./coglet/cmd/coglet-server

# Build all coglet-server binaries (for wheel embedding)
.PHONY: coglet-server-binaries
coglet-server-binaries: $(COGLET_GO_SOURCE)
	@rm -f $(COGLET_BINARY_DIR)/cog-*
	@for os in darwin linux; do \
		for arch in amd64 arm64; do \
			echo "Building coglet-server for $$os/$$arch"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o $(COGLET_BINARY_DIR)/cog-$$os-$$arch ./coglet/cmd/coglet-server; \
		done; \
	done

# Build coglet wheel (includes embedded Go binaries)
.PHONY: coglet-wheel
coglet-wheel: coglet-server-binaries
	cd coglet && $(UV) build --wheel --out-dir=dist .

# Run coglet Go tests
.PHONY: test-coglet-go
test-coglet-go:
	go run gotest.tools/gotestsum@latest --format dots-v2 ./coglet/... -- -timeout=30s $(ARGS)

# Run coglet Python tests (requires coglet to be installed)
.PHONY: test-coglet-python
test-coglet-python:
	cd coglet && $(UV) run pytest python/tests $(ARGS)

# Run all coglet tests
.PHONY: test-coglet
test-coglet: test-coglet-go test-coglet-python

# Clean coglet build artifacts
.PHONY: clean-coglet
clean-coglet:
	rm -rf coglet/dist coglet/build
	rm -f $(COGLET_BINARY_DIR)/cog-*
