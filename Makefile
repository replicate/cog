SHELL := bash

DESTDIR ?=
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

INSTALL := install -m 0755
INSTALL_PROGRAM := $(INSTALL)

GO ?= go
GOOS := $(shell $(GO) env GOOS)
GOARCH := $(shell $(GO) env GOARCH)

PYTHON ?= python
PYTEST := $(PYTHON) -m pytest
PYRIGHT := $(PYTHON) -m pyright
RUFF := $(PYTHON) -m ruff

default: all

.PHONY: all
all: cog

pkg/dockerfile/embed/cog.whl: python/* python/cog/* python/cog/server/* python/cog/command/*
	@echo "Building Python library"
	rm -rf dist
	$(PYTHON) -m pip install build && $(PYTHON) -m build --wheel
	mkdir -p pkg/dockerfile/embed
	cp dist/*.whl $@

.PHONY: cog
cog: pkg/dockerfile/embed/cog.whl
	$(eval COG_VERSION ?= $(shell git describe --tags --match 'v*' --abbrev=0)+dev)
	CGO_ENABLED=0 $(GO) build -o $@ \
		-ldflags "-X github.com/replicate/cog/pkg/global.Version=$(COG_VERSION) -X github.com/replicate/cog/pkg/global.BuildTime=$(shell date +%Y-%m-%dT%H:%M:%S%z) -w" \
		cmd/cog/cog.go

.PHONY: base-image
base-image: pkg/dockerfile/embed/cog.whl
	$(eval COG_VERSION ?= $(shell git describe --tags --match 'v*' --abbrev=0)+dev)
	CGO_ENABLED=0 $(GO) build -o $@ \
		-ldflags "-X github.com/replicate/cog/pkg/global.Version=$(COG_VERSION) -X github.com/replicate/cog/pkg/global.BuildTime=$(shell date +%Y-%m-%dT%H:%M:%S%z) -w" \
		cmd/base-image/baseimage.go

.PHONY: install
install: cog
	$(INSTALL_PROGRAM) -d $(DESTDIR)$(BINDIR)
	$(INSTALL_PROGRAM) cog $(DESTDIR)$(BINDIR)/cog

.PHONY: uninstall
uninstall:
	rm -f $(DESTDIR)$(BINDIR)/cog

.PHONY: clean
clean:
	$(GO) clean
	rm -rf build dist
	rm -f cog
	rm -f pkg/dockerfile/embed/cog.whl

.PHONY: test-go
test-go: pkg/dockerfile/embed/cog.whl | check-fmt vet lint-go
	$(GO) get gotest.tools/gotestsum
	$(GO) run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

.PHONY: test-integration
test-integration: cog
	cd test-integration/ && $(MAKE) PATH="$(PWD):$(PATH)" test

.PHONY: test-python
test-python:
	$(PYTEST) -n auto -vv --cov=python/cog  --cov-report term-missing  python/tests $(if $(FILTER),-k "$(FILTER)",)

.PHONY: test
test: test-go test-python test-integration


.PHONY: fmt
fmt:
	$(GO) run golang.org/x/tools/cmd/goimports -w -d .

.PHONY: generate
generate:
	$(GO) generate ./...


.PHONY: vet
vet:
	$(GO) vet ./...


.PHONY: check-fmt
check-fmt:
	$(GO) run golang.org/x/tools/cmd/goimports -d .
	@test -z $$($(GO) run golang.org/x/tools/cmd/goimports -l .)

.PHONY: lint-go
lint-go:
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

.PHONY: lint-python
lint-python:
	$(RUFF) check python/cog
	$(RUFF) format --check python
	@$(PYTHON) -c 'import sys; sys.exit("Warning: python >=3.10 is needed (not installed) to pass linting (pyright)") if sys.version_info < (3, 10) else None'
	$(PYRIGHT)

.PHONY: lint
lint: lint-go lint-python

.PHONY: mod-tidy
mod-tidy:
	$(GO) mod tidy

.PHONY: install-python # install dev dependencies
install-python:
	$(PYTHON) -m pip install '.[dev]'


.PHONY: run-docs-server
run-docs-server:
	pip install mkdocs-material
	sed 's/docs\///g' README.md > ./docs/README.md
	cp CONTRIBUTING.md ./docs/
	mkdocs serve
