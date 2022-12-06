SHELL := /bin/bash

COG_VERSION ?= $(shell git describe --tags --match 'v*' --abbrev=0)+dev
RELEASE_DIR := release
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY := $(RELEASE_DIR)/$(GOOS)/$(GOARCH)/cog
INSTALL_PATH := /usr/local/bin/cog
MAIN := cmd/cog/cog.go
BUILD_TIME := $(shell date +%Y-%m-%dT%H:%M:%S%z)
LDFLAGS := -ldflags "-X github.com/replicate/cog/pkg/global.Version=$(COG_VERSION) -X github.com/replicate/cog/pkg/global.BuildTime=$(BUILD_TIME) -w"

default: all

.PHONY: all
all: cog

pkg/dockerfile/embed/cog.whl: python/* python/cog/* python/cog/server/* python/cog/command/*
	@echo "Building Python library"
	rm -rf python/dist
	cd python && python setup.py bdist_wheel
	mkdir -p pkg/dockerfile/embed
	cp python/dist/*.whl pkg/dockerfile/embed/cog.whl

cog: pkg/dockerfile/embed/cog.whl
	CGO_ENABLED=0 go build $(LDFLAGS) -o $@ cmd/cog/cog.go

.PHONY: install
install: cog

.PHONY: clean
clean:
	rm -f cog
	rm -f pkg/dockerfile/embed/cog.whl

.PHONY: generate
generate:
	go generate ./...

.PHONY: test-go
test-go: build-dependencies check-fmt vet lint
	go get gotest.tools/gotestsum
	go run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

# TODO(bfirsh): use local copy of cog so we don't have to install globally
.PHONY: test-integration
test-integration: cog
	cd test-integration/ && $(MAKE)

.PHONY: test-python
test-python:
	cd python/ && pytest -vv


.PHONY: test
test: test-go test-python test-integration

.PHONY: install
install: build-dependencies
	go install $(LDFLAGS) $(MAIN)

.PHONY: fmt
fmt:
	go run golang.org/x/tools/cmd/goimports -w -d .

.PHONY: vet
vet:
	go vet ./...


.PHONY: check-fmt
check-fmt:
	go run golang.org/x/tools/cmd/goimports -d .
	@test -z $$(go run golang.org/x/tools/cmd/goimports -l .)

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...
	mypy python/cog 

.PHONY: mod-tidy
mod-tidy:
	go mod tidy
