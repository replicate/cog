SHELL := bash

DESTDIR ?=
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

INSTALL := install -m 0755

GO ?= go
# goreleaser v2.3.0 requires go 1.23; PR #1950 is where we're doing that. For
# now, pin to v2.2.0
GORELEASER := $(GO) run github.com/goreleaser/goreleaser/v2@v2.2.0

PYTHON ?= python
TOX := $(PYTHON) -Im tox

COG_GO_SOURCE := $(shell find cmd pkg -type f)
COG_PYTHON_SOURCE := $(shell find python/cog -type f -name '*.py')

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
	$(PYTHON) -m pip wheel --no-deps --no-binary=:all: --wheel-dir=pkg/dockerfile/embed .
	@touch $@
endif

$(COG_BINARIES): $(COG_GO_SOURCE) pkg/dockerfile/embed/.wheel
	$(GORELEASER) build --clean --snapshot --single-target --id $@ --output $@

.PHONY: install
install: $(COG_BINARIES)
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) $< $(DESTDIR)$(BINDIR)/$<

.PHONY: clean
clean:
	rm -rf build dist pkg/dockerfile/embed
	rm -f $(COG_BINARIES)

.PHONY: test-go
test-go: $(COG_EMBEDDED_WHEEL)
	$(GO) get gotest.tools/gotestsum
	$(GO) run gotest.tools/gotestsum -- -timeout 1200s -parallel 5 ./... $(ARGS)

.PHONY: test-integration
test-integration: $(COG_BINARIES)
	PATH="$(PWD):$(PATH)" $(TOX) -e integration

.PHONY: test-python
test-python: $(COG_WHEEL)
	$(TOX) run --installpkg $(COG_WHEEL) -f tests

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
lint: $(COG_EMBEDDED_WHEEL) $(COG_WHEEL) check-fmt vet
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...
	$(TOX) run --installpkg $(COG_WHEEL) -e lint,typecheck-pydantic2

.PHONY: run-docs-server
run-docs-server:
	pip install mkdocs-material
	sed 's/docs\///g' README.md > ./docs/README.md
	cp CONTRIBUTING.md ./docs/
	mkdocs serve
