SHELL := /bin/bash

PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

INSTALL := install -m 0755
INSTALL_PROGRAM := $(INSTALL)

GO := go
GOOS := $(shell $(GO) env GOOS)
GOARCH := $(shell $(GO) env GOARCH)

PYTHON := python
PYTEST := pytest
MYPY := mypy

COG_VERSION ?= $(shell git describe --tags --match 'v*' --abbrev=0)+dev
BUILD_TIME := $(shell date +%Y-%m-%dT%H:%M:%S%z)
LDFLAGS := -ldflags "-X github.com/replicate/cog/pkg/global.Version=$(COG_VERSION) -X github.com/replicate/cog/pkg/global.BuildTime=$(BUILD_TIME) -w"

default: all

.PHONY: all
all: cog

pkg/dockerfile/embed/cog.whl: python/* python/cog/* python/cog/server/* python/cog/command/*
	@echo "Building Python library"
	rm -rf python/dist
	cd python && $(PYTHON) setup.py bdist_wheel
	mkdir -p pkg/dockerfile/embed
	cp python/dist/*.whl $@

cog: pkg/dockerfile/embed/cog.whl
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o $@ cmd/cog/cog.go

.PHONY: install
install: cog
	$(INSTALL_PROGRAM) cog $(BINDIR)/cog

.PHONY: uninstall
uninstall:
	rm -f $(DESTDIR)$(BINDIR)/cog

.PHONY: clean
clean:
	$(GO) clean
	rm -f cog
	rm -f pkg/dockerfile/embed/cog.whl

.PHONY: test-go
test-go: pkg/dockerfile/embed/cog.whl | check-fmt vet lint
	$(GO) get gotest.tools/gotestsum
	$(GO) run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

.PHONY: test-integration
test-integration: cog
	cd test-integration/ && $(MAKE) PATH="$(PWD):$(PATH)" test

.PHONY: test-python
test-python:
	cd python/ && $(PYTEST) -vv

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

.PHONY: lint
lint:
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...
	$(MYPY) python/cog

.PHONY: mod-tidy
mod-tidy:
	$(GO) mod tidy
