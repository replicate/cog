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
		$(GORELEASER) build --clean --snapshot --single-target --id $@ --output $@; \
	else \
		$(GORELEASER) build --clean --auto-snapshot --single-target --id $@ --output $@; \
	fi

.PHONY: install
install: $(COG_BINARIES)
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) $< $(DESTDIR)$(BINDIR)/$<

.PHONY: clean
clean:
	rm -rf .tox build dist pkg/dockerfile/embed
	rm -f $(COG_BINARIES)

.PHONY: test-go
test-go: pkg/dockerfile/embed/.wheel
	$(GO) tool gotestsum -- -short -timeout 1200s -parallel 5 ./... $(ARGS)

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
