SHELL := /bin/bash
.DEFAULT_GOAL := build

GOFUMPT_VERSION := v0.11.0
GOIMPORTS_VERSION := v0.48.0
GOLANGCI_LINT_VERSION := v2.11.4
OAPI_CODEGEN_VERSION := v2.8.0
DOCKER ?= docker
BACKEND_IMAGE ?= retrom
WEB_IMAGE ?= retrom-web
IMAGE_TAG ?= latest
RETROM_DEPENDENCY_VERSIONS ?= 4.2.3,4.3.0-pre
RETROM_ACTIVE_EMULATORJS_VERSION ?= 4.2.3
RETROM_DEPENDENCY_ROOT ?= $(abspath data)
RETROM_DATA_DIR ?= $(abspath .cache/retrom/user-management-v1-data)
RETROM_MODE ?= test
RETROM_HTTP_ADDR ?= 127.0.0.1:8080
RETROM_PUBLIC_ORIGIN ?= https://dev.sendev.cc
RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN ?= true
RETROM_MULTI_DISC_IMPORT_ENABLED ?= true
RETROM_NETPLAY_ENABLED ?= true
RETROM_NETPLAY_MAX_ACTIVE_ROOMS ?= 16
NEXT_DEV_HOST ?= 0.0.0.0
NEXT_DEV_PORT ?= 3000
NEXT_BACKEND_ORIGIN ?= http://$(RETROM_HTTP_ADDR)
NODE_HOME := $(abspath .cache/tools/node-v24.18.0-linux-x64)
NPM := PATH="$(NODE_HOME)/bin:$$PATH" npm
PLAYWRIGHT_BROWSERS_PATH ?= $(abspath .cache/tools/ms-playwright)
RETROM_CHROME_EXECUTABLE ?= $(abspath .cache/tools/retrom-chrome-for-testing)
export PLAYWRIGHT_BROWSERS_PATH
export RETROM_CHROME_EXECUTABLE

GO_PACKAGES := ./cmd/... ./internal/... ./migrations/...

.PHONY: fmt fmt-check install-deps install-go-formatters install-golangci-lint prepare-node prepare-e2e-browser \
	build test lint-go backend-check web-install web-lint web-typecheck web-test web-build web-check integration-test api-generate api-check \
	public-fixtures-generate public-fixtures-check web-e2e data-check prepare-deps deps-check release-input-digest ci dev build-backend-image \
	build-web-image build-images acceptance-prepare acceptance-case acceptance-report

install-deps: install-go-formatters install-golangci-lint prepare-deps web-install prepare-e2e-browser public-fixtures-check
	@go mod download

install-go-formatters:
	@mkdir -p bin
	@if [[ ! -x bin/gofumpt ]] || [[ "$$(bin/gofumpt -version 2>&1)" != *"$(GOFUMPT_VERSION:v%=%)"* ]]; then GOBIN="$(abspath bin)" go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION); fi
	@if [[ ! -x bin/goimports ]]; then GOBIN="$(abspath bin)" go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION); fi

install-golangci-lint:
	@mkdir -p bin
	@if [[ ! -x bin/golangci-lint ]] || [[ "$$(bin/golangci-lint version 2>&1)" != *"$(GOLANGCI_LINT_VERSION:v%=%)"* ]]; then GOBIN="$(abspath bin)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); fi

fmt: install-go-formatters
	@bin/gofumpt -w $$(find cmd internal migrations -name '*.go' -type f ! -path '*/generated/*' | sort)
	@bin/goimports -w $$(find cmd internal migrations -name '*.go' -type f ! -path '*/generated/*' | sort)

fmt-check: install-go-formatters
	@scripts/fmt-check.sh

build:
	@go build ./cmd/retrom

test:
	@go test $(GO_PACKAGES)

lint-go: install-golangci-lint
	@bin/golangci-lint run $(GO_PACKAGES)

backend-check: fmt-check build test lint-go

prepare-node:
	@scripts/prepare-node.sh

web-install: prepare-node
	@cd web && $(NPM) ci

prepare-e2e-browser: web-install
	@PATH="$(NODE_HOME)/bin:$$PATH" PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_BROWSERS_PATH)" scripts/prepare-e2e-browser.sh

web-lint: prepare-node
	@cd web && $(NPM) run lint

web-typecheck: prepare-node
	@cd web && $(NPM) run typecheck

web-test: prepare-node
	@cd web && $(NPM) run test:ci

NEXT_DIST_DIR ?= .next

web-build: prepare-node
	@test "$(NEXT_DIST_DIR)" = ".next" || test "$(NEXT_DIST_DIR)" = ".next-build"
	@cd web && rm -rf "$(NEXT_DIST_DIR)" && NEXT_DIST_DIR="$(NEXT_DIST_DIR)" $(NPM) run build

web-check: web-install web-lint web-typecheck web-test web-build

integration-test:
	@go test -tags=integration $(GO_PACKAGES)

api-generate: web-install
	@go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) --config api/oapi-codegen.yaml api/openapi.yaml
	@cd web && $(NPM) run api:generate

api-check: web-install
	@scripts/api-check.sh

public-fixtures-generate:
	@python3 testdata/public-roms/gba-smoke/build.py
	@python3 testdata/public-roms/arcade-smoke/build.py

public-fixtures-check:
	@python3 testdata/public-roms/gba-smoke/build.py --check
	@python3 testdata/public-roms/arcade-smoke/build.py --check

web-e2e: prepare-e2e-browser public-fixtures-check
	@PATH="$(NODE_HOME)/bin:$$PATH" scripts/acceptance/web-e2e.sh

data-check:
	@python3 scripts/test_makefile.py
	@python3 scripts/test_workflows.py
	@python3 scripts/test_design_assets.py
	@python3 scripts/test_public_fixtures.py
	@python3 scripts/test_dependencies.py
	@python3 scripts/test_fbalpha2012_dat.py
	@python3 scripts/dependencies.py data-check --versions "$(RETROM_DEPENDENCY_VERSIONS)"

prepare-deps:
	@python3 scripts/dependencies.py prepare --versions "$(RETROM_DEPENDENCY_VERSIONS)"

deps-check:
	@python3 scripts/dependencies.py deps-check --versions "$(RETROM_DEPENDENCY_VERSIONS)"

release-input-digest:
	@python3 scripts/release-input-digest.py --versions "$(RETROM_DEPENDENCY_VERSIONS)" --active "$(RETROM_ACTIVE_EMULATORJS_VERSION)"

ci: api-check backend-check web-check integration-test data-check

dev: prepare-deps web-install
	@RETROM_HTTP_ADDR="$(RETROM_HTTP_ADDR)" \
	 RETROM_PUBLIC_ORIGIN="$(RETROM_PUBLIC_ORIGIN)" \
	 RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN="$(RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN)" \
	 RETROM_MULTI_DISC_IMPORT_ENABLED="$(RETROM_MULTI_DISC_IMPORT_ENABLED)" \
	 RETROM_NETPLAY_ENABLED="$(RETROM_NETPLAY_ENABLED)" \
	 RETROM_NETPLAY_MAX_ACTIVE_ROOMS="$(RETROM_NETPLAY_MAX_ACTIVE_ROOMS)" \
	 RETROM_MODE="$(RETROM_MODE)" \
	 RETROM_DATA_DIR="$(RETROM_DATA_DIR)" \
	 RETROM_DEPENDENCY_ROOT="$(RETROM_DEPENDENCY_ROOT)" \
	 RETROM_DEPENDENCY_VERSIONS="$(RETROM_DEPENDENCY_VERSIONS)" \
	 RETROM_ACTIVE_EMULATORJS_VERSION="$(RETROM_ACTIVE_EMULATORJS_VERSION)" \
	 NEXT_DEV_HOST="$(NEXT_DEV_HOST)" \
	 NEXT_DEV_PORT="$(NEXT_DEV_PORT)" \
	 NEXT_BACKEND_ORIGIN="$(NEXT_BACKEND_ORIGIN)" \
	 PATH="$(NODE_HOME)/bin:$$PATH" scripts/dev.sh

build-backend-image: data-check
	@scripts/build-image.sh backend "$(BACKEND_IMAGE):$(IMAGE_TAG)" "$(RETROM_DEPENDENCY_VERSIONS)" "$(RETROM_ACTIVE_EMULATORJS_VERSION)" "$(DOCKER)"

build-web-image: data-check
	@scripts/build-image.sh web "$(WEB_IMAGE):$(IMAGE_TAG)" "$(RETROM_DEPENDENCY_VERSIONS)" "$(RETROM_ACTIVE_EMULATORJS_VERSION)" "$(DOCKER)"

build-images: build-backend-image build-web-image
	@set -euo pipefail; \
	 expected="$$(python3 scripts/release-input-digest.py --versions "$(RETROM_DEPENDENCY_VERSIONS)" --active "$(RETROM_ACTIVE_EMULATORJS_VERSION)")"; \
	 backend="$$( $(DOCKER) image inspect --format '{{ index .Config.Labels "io.retrom.release-input-sha256" }}' "$(BACKEND_IMAGE):$(IMAGE_TAG)" )"; \
	 web="$$( $(DOCKER) image inspect --format '{{ index .Config.Labels "io.retrom.release-input-sha256" }}' "$(WEB_IMAGE):$(IMAGE_TAG)" )"; \
	 [[ "$$backend" == "$$expected" && "$$web" == "$$expected" ]]

acceptance-prepare:
	@scripts/acceptance/run.sh prepare

acceptance-case:
	@test -n "$(CASE)" || { echo 'CASE is required' >&2; exit 2; }
	@scripts/acceptance/run.sh case "$(CASE)"

acceptance-report:
	@scripts/acceptance/run.sh report
