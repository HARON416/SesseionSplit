.DEFAULT_GOAL := help

# Wails tasks invoke wails3 themselves, so expose the project-local CLI.
export PATH := $(CURDIR)/.tools/bin:$(PATH)
WAILS_VERSION := $(shell awk '$$1 == "require" && $$2 == "github.com/wailsapp/wails/v3" {print $$3} $$1 == "github.com/wailsapp/wails/v3" {print $$2}' go.mod)
WAILS_CLI := .tools/bin/wails3
VITE_PORT ?= $(if $(WAILS_VITE_PORT),$(WAILS_VITE_PORT),9246)

.PHONY: help dev run build check doctor install-cli

help:
	@printf '%s\n' 'SessionSplit commands:' '  make dev          Start the desktop app with hot reload' '  make run          Alias for make dev' '  make build        Build the desktop executable' '  make check        Check Svelte and TypeScript' '  make doctor       Check Wails dependencies' '  make install-cli  Install the pinned Wails CLI locally'

dev run: $(WAILS_CLI)
	wails3 dev -config ./build/config.yml -port $(VITE_PORT)

build: $(WAILS_CLI)
	wails3 build

check:
	npm --prefix frontend run check

doctor: $(WAILS_CLI)
	wails3 doctor

$(WAILS_CLI): go.mod
	CGO_ENABLED=0 GOBIN="$(CURDIR)/.tools/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS_VERSION)

install-cli:
	CGO_ENABLED=0 GOBIN="$(CURDIR)/.tools/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS_VERSION)
