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
	$(GO) tool gotestsum -- -short -timeout 1200s -parallel 5 $$(go list ./... | grep -v 'coglet/') $(ARGS)

.PHONY: test-integration
test-integration: $(COG_BINARIES)
	$(GO) test ./pkg/docker/...
	PATH="$(PWD):$(PATH)" $(TOX) -e integration

.PHONY: test-python
test-python: generate
	$(TOX) run --installpkg $$(ls dist/cog-*.whl) -f tests

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
	rm -rf coglet/dist coglet/build coglet/python/cog/bin
