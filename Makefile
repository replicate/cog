SHELL := /bin/bash

VERSION := 0.0.1
RELEASE_DIR := release
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY := $(RELEASE_DIR)/$(GOOS)/$(GOARCH)/cog
INSTALL_PATH := /usr/local/bin/cog
MAIN := cmd/cog/cog.go
LDFLAGS := -ldflags "-X github.com/replicate/cog/pkg/global.Version=$(VERSION) -w"

.PHONY: build
build: clean
	@mkdir -p $(RELEASE_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(MAIN)

.PHONY: clean
clean:
	rm -rf $(RELEASE_DIR)

.PHONY: generate
generate:
	go generate ./...

.PHONY: test
test:
	go run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

.PHONY: install
install: build
	go install $(MAIN)

.PHONY: fmt
fmt:
	go run golang.org/x/tools/cmd/goimports --local github.com/replicate/cog -w -d .

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

.PHONY: mod-tidy
mod-tidy:
	go mod tidy
