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

.PHONY: test-go
test-go: check-fmt vet lint
	go get gotest.tools/gotestsum
	go run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

.PHONY: test-end-to-end
test-end-to-end: install
	cd end-to-end-test/ && $(MAKE)

.PHONY: test-cog-library
test-cog-library:
	cd pkg/docker/ && pytest cog_test.py

.PHONY: test
test: test-go test-cog-library test-end-to-end

.PHONY: install
install:
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
