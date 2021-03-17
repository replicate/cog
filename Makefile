SHELL := /bin/bash

VERSION := 0.0.1
RELEASE_DIR := release
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY := $(RELEASE_DIR)/$(GOOS)/$(GOARCH)/cog
INSTALL_PATH := /usr/local/bin/cog
MAIN := cmd/cog/main.go
LDFLAGS := -ldflags "-X github.com/replicate/replicate/go/pkg/global.Version=$(VERSION) -w"

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
	sudo cp $(BINARY) $(INSTALL_PATH)
