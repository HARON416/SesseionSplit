.DEFAULT_GOAL := help

# Wails tasks invoke wails3 themselves, so expose the project-local CLI.
export PATH := $(CURDIR)/.tools/bin:$(PATH)
WAILS_VERSION := $(shell awk '$$1 == "require" && $$2 == "github.com/wailsapp/wails/v3" {print $$3} $$1 == "github.com/wailsapp/wails/v3" {print $$2}' go.mod)
WAILS_CLI := .tools/bin/wails3
VITE_PORT ?= $(if $(WAILS_VITE_PORT),$(WAILS_VITE_PORT),9246)

.PHONY: help dev run build clean check doctor install-cli

help:
	@printf '%s\n' 'Isolate commands:' '  make dev          Start the desktop app with hot reload' '  make run          Alias for make dev' '  make build        Build the desktop executable' '  make linux-deb                  Build the standard Ubuntu DEB' '  make linux-bundled-deb          Build Ubuntu DEB with Java and scrcpy' '  make windows-installer          Build the standard Windows installer' '  make windows-bundled-installer  Build with Java and scrcpy included' '  make clean        Remove generated executables and packages from bin/' '  make check        Check Svelte and TypeScript' '  make doctor       Check Wails dependencies' '  make install-cli  Install the pinned Wails CLI locally'

dev run: $(WAILS_CLI)
	wails3 dev -config ./build/config.yml -port $(VITE_PORT)

build: $(WAILS_CLI)
	wails3 build

clean:
	rm -rf -- bin

check:
	npm --prefix frontend run check

doctor: $(WAILS_CLI)
	wails3 doctor

$(WAILS_CLI): go.mod
	CGO_ENABLED=0 GOBIN="$(CURDIR)/.tools/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS_VERSION)

install-cli:
	CGO_ENABLED=0 GOBIN="$(CURDIR)/.tools/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS_VERSION)

# The bundled-tools edition includes Java/scrcpy plus matching sources and
# notices. Google SDK components and Play images still download on the client.
.PHONY: windows-installer windows-bundled-installer prepare-windows-runtime
windows-installer: $(WAILS_CLI)
	wails3 package GOOS=windows ARCH=amd64 CGO_ENABLED=0 INSTALL_SCOPE=user INSTALLER_FLAVOR=standard

prepare-windows-runtime:
	go run ./build/runtime-bundle -out .tools/runtime/windows-amd64

windows-bundled-installer: $(WAILS_CLI) prepare-windows-runtime
	wails3 package GOOS=windows ARCH=amd64 CGO_ENABLED=0 INSTALL_SCOPE=user INSTALLER_FLAVOR=bundled-tools

.PHONY: linux-deb linux-bundled-deb prepare-linux-runtime
linux-deb: $(WAILS_CLI)
	wails3 package GOOS=linux ARCH=amd64 INSTALLER_FLAVOR=standard

prepare-linux-runtime:
	go run ./build/runtime-bundle -target linux/amd64 -out .tools/runtime/linux-amd64

linux-bundled-deb: $(WAILS_CLI) prepare-linux-runtime
	wails3 package GOOS=linux ARCH=amd64 INSTALLER_FLAVOR=bundled-tools
