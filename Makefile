SHELL := /bin/bash

VERSION := 0.0.1
RELEASE_DIR := release
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY := $(RELEASE_DIR)/$(GOOS)/$(GOARCH)/cog
INSTALL_PATH := /usr/local/bin/cog
MAIN := cmd/cog/cog.go
BUILD_TIME := $(shell date +%Y-%m-%dT%H:%M:%S%z)
LDFLAGS := -ldflags "-X github.com/replicate/cog/pkg/global.Version=$(VERSION) -X github.com/replicate/cog/pkg/global.BuildTime=$(BUILD_TIME) -w"


pkg/dockerfile/embed/cog.whl: python/* python/cog/*
	@echo "Building Python library"
	rm -rf python/dist
	cd python && python setup.py bdist_wheel
	mkdir -p pkg/dockerfile/embed 
	cp python/dist/*.whl pkg/dockerfile/embed/cog.whl

go-dependencies: pkg/dockerfile/embed/cog.whl

.PHONY: build
build: clean go-dependencies
	@mkdir -p $(RELEASE_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(MAIN)

.PHONY: clean
clean:
	rm -rf $(RELEASE_DIR)

.PHONY: generate
generate:
	go generate ./...

.PHONY: test-go
test-go: go-dependencies check-fmt vet lint
	go get gotest.tools/gotestsum
	go run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

# TODO(bfirsh): use local copy of cog so we don't have to install globally
.PHONY: test-integration
test-integration: install
	cd test-integration/ && $(MAKE)

.PHONY: test-python
test-python:
	cd python/ && pytest

.PHONY: test
test: test-go test-python test-integration

.PHONY: install
install: go-dependencies
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

.PHONY: mod-tidy
mod-tidy:
	go mod tidy
