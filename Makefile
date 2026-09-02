SHELL := /bin/bash
.DEFAULT_GOAL := build

GOFUMPT_VERSION := v0.11.0
GOIMPORTS_VERSION := v0.48.0
GOLANGCI_LINT_VERSION := v2.11.4
OAPI_CODEGEN_VERSION := v2.8.0
GO_VERSION := $(shell awk '$$1 == "go" { print $$2; exit }' go.mod)
DOCKER ?= docker
BACKEND_IMAGE ?= retrom
WEB_IMAGE ?= retrom-web
IMAGE_TAG ?= latest
RETROM_DEV_CONFIG ?= $(abspath .dev-data/dev.mk)
-include $(RETROM_DEV_CONFIG)

RETROM_RUNTIME_DEV_ROOT ?=
RETROM_RUNTIME_DEV_INCLUDE_ASSETS ?= false
RETROM_RUNTIME_MATERIALIZATION_ROOT ?= $(RETROM_DEPENDENCY_ROOT)/runtime/rpgmaker/v1
RETROM_RUNTIME_PFB_CANDIDATE_ROOT ?=
RETROM_PROVIDER_LOCK_ROOT ?= $(abspath data/runtime-providers)
RETROM_PROVIDER_CACHE_ROOT ?= $(abspath .cache/runtime-providers)
RETROM_PROVIDER_INSTALLED_ROOT ?= $(abspath data/runtime-providers/installed)
RETROM_PROVIDER_ACTIVE_PATH ?= $(abspath data/runtime-providers/active.json)
RETROM_PROVIDER_SOURCE ?= production
RETROM_DEPENDENCY_VERSIONS ?= 4.2.3,4.3.0-pre
RETROM_ACTIVE_EMULATORJS_VERSION ?= 4.2.3
RETROM_DEPENDENCY_ROOT ?= $(abspath data)
RETROM_DEV_STATE_DIR ?= $(abspath .dev-data/dev-state)
RETROM_DATA_DIR ?= $(abspath .dev-data/data)
RETROM_MODE ?= test
RETROM_HTTP_ADDR ?= 127.0.0.1:8080
RETROM_PUBLIC_ORIGIN ?= http://localhost:4000
RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN ?= true
RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE ?= http://{launchId}.rpg.localhost:8080
RETROM_MULTI_DISC_IMPORT_ENABLED ?= true
RETROM_NETPLAY_ENABLED ?= true
RETROM_NETPLAY_MAX_ACTIVE_ROOMS ?= 16
NEXT_DEV_HOST ?= 127.0.0.1
NEXT_DEV_PORT ?= 4000
NEXT_BACKEND_ORIGIN ?= http://$(RETROM_HTTP_ADDR)
NEXT_DEV_DIST_DIR ?= $(if $(strip $(RETROM_RUNTIME_DEV_ROOT)),.next-runtime-dev,.next)
NODE_HOME ?= $(abspath .cache/tools/node-v24.18.0-linux-x64)
NODE_PREPARE_MODE ?= repository
NPM := PATH="$(NODE_HOME)/bin:$$PATH" npm
GO_HOME ?= $(abspath .cache/tools/go$(GO_VERSION)-linux-amd64)
GO_PREPARE_MODE ?= auto
RETROM_HOST_PATH := $(PATH)
ifneq ($(GO_PREPARE_MODE),system)
export PATH := $(GO_HOME)/bin:$(RETROM_HOST_PATH)
endif
PLAYWRIGHT_BROWSERS_PATH ?= $(abspath .cache/tools/ms-playwright)
RETROM_CHROME_EXECUTABLE ?= $(abspath .cache/tools/retrom-chrome-for-testing)
export PLAYWRIGHT_BROWSERS_PATH
export RETROM_CHROME_EXECUTABLE

GO_PACKAGES := ./cmd/... ./internal/... ./migrations/...
API_OPENAPI_SOURCES := api/openapi.yaml $(sort $(wildcard api/domains/*.yaml api/components/*.yaml))
API_CODEGEN_CONFIGS := $(sort $(wildcard api/codegen/*.yaml))
API_BUNDLE := .cache/generated/openapi.bundle.yaml
API_GO_GENERATED := internal/httpapi/generated/models.gen.go internal/httpapi/generated/server.gen.go internal/httpapi/generated/spec.gen.go

.PHONY: fmt fmt-check quality-structure-check install-deps install-go-formatters install-golangci-lint prepare-go prepare-node prepare-e2e-browser \
	build test lint-go backend-check web-install web-lint web-typecheck web-test web-build web-check integration-test api-bundle api-generate-go api-generate api-check \
	public-fixtures-generate public-fixtures-check web-e2e data-check prepare-deps deps-check release-input-digest ci dev build-backend-image \
	build-web-image build-images acceptance-prepare acceptance-case acceptance-report retrom-runtime-dev-link retrom-runtime-dev-unlink \
	runtime-provider-prepare runtime-provider-prepare-candidate runtime-provider-check runtime-provider-pin-release runtime-provider-verify-upgrade \
	require-local-user pfb-init pfb-validate pfb-build pfb-up pfb-use pfb-restart pfb-down pfb-status pfb-logs pfb-verify pfb-prune pfb-destroy pfb-gateway-up pfb-gateway-down

.NOTPARALLEL: dev

install-deps: prepare-go install-go-formatters install-golangci-lint prepare-deps web-install prepare-e2e-browser public-fixtures-check
	@go mod download

install-go-formatters: prepare-go
	@mkdir -p bin
	@if [[ ! -x bin/gofumpt ]] || [[ "$$(bin/gofumpt -version 2>&1)" != *"$(GOFUMPT_VERSION:v%=%)"* ]]; then GOBIN="$(abspath bin)" go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION); fi
	@if [[ ! -x bin/goimports ]]; then GOBIN="$(abspath bin)" go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION); fi

install-golangci-lint: prepare-go
	@mkdir -p bin
	@if [[ ! -x bin/golangci-lint ]] || [[ "$$(bin/golangci-lint version 2>&1)" != *"$(GOLANGCI_LINT_VERSION:v%=%)"* ]]; then GOBIN="$(abspath bin)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); fi

fmt: install-go-formatters
	@bin/gofumpt -w $$(find cmd internal migrations -name '*.go' -type f ! -path '*/generated/*' | sort)
	@bin/goimports -w $$(find cmd internal migrations -name '*.go' -type f ! -path '*/generated/*' | sort)

fmt-check: install-go-formatters
	@scripts/fmt-check.sh

quality-structure-check:
	@python3 scripts/test_quality_structure.py
	@python3 scripts/quality_structure.py

api-bundle: $(API_BUNDLE)

$(API_BUNDLE): $(API_OPENAPI_SOURCES) scripts/openapi-bundle/main.go go.mod go.sum | prepare-go
	@mkdir -p $(@D)
	@go run ./scripts/openapi-bundle -input api/openapi.yaml -output $@

api-generate-go: $(API_GO_GENERATED)

$(API_GO_GENERATED) &: $(API_BUNDLE) $(API_CODEGEN_CONFIGS) go.mod go.sum | prepare-go
	@mkdir -p $(@D)
	@for config in $(API_CODEGEN_CONFIGS); do \
		go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) --config "$$config" $(API_BUNDLE); \
	done

build: prepare-go api-generate-go
	@go build ./cmd/retrom

test: prepare-go api-generate-go
	@go test $(GO_PACKAGES)

lint-go: api-generate-go install-golangci-lint
	@bin/golangci-lint run $(GO_PACKAGES)

backend-check: quality-structure-check fmt-check build test lint-go

prepare-go:
	@if [[ "$(GO_PREPARE_MODE)" = system ]]; then \
		PATH="$(RETROM_HOST_PATH)"; export PATH; \
		test "$$(go env GOVERSION 2>/dev/null)" = "go$(GO_VERSION)" || { echo 'system Go version mismatch' >&2; exit 1; }; \
	elif [[ "$(GO_PREPARE_MODE)" = auto ]] && command -v go >/dev/null 2>&1 && [[ "$$(go env GOVERSION 2>/dev/null)" = "go$(GO_VERSION)" ]]; then \
		:; \
	else \
		test "$(GO_PREPARE_MODE)" = auto || test "$(GO_PREPARE_MODE)" = repository || { echo 'GO_PREPARE_MODE must be auto, repository or system' >&2; exit 2; }; \
		scripts/prepare-go.sh; \
	fi

prepare-node:
	@if [[ "$(NODE_PREPARE_MODE)" = system ]]; then \
		test "$$(node --version)" = "v$$(cat .node-version)" && test "$$(npm --version)" = "11.16.0" || { echo 'system Node/npm version mismatch' >&2; exit 1; }; \
	else \
		test "$(NODE_PREPARE_MODE)" = repository || { echo 'NODE_PREPARE_MODE must be repository or system' >&2; exit 2; }; \
		scripts/prepare-node.sh; \
	fi

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

web-check: quality-structure-check web-install web-lint web-typecheck web-test web-build

integration-test: prepare-go api-generate-go
	@go test -tags=integration $(GO_PACKAGES)

api-generate: api-generate-go web-install
	@cd web && $(NPM) run api:generate

api-check: prepare-go web-install
	@scripts/api-check.sh

public-fixtures-generate:
	@python3 testdata/public-roms/gba-smoke/build.py
	@python3 testdata/public-roms/nes-smoke/build.py
	@python3 testdata/public-roms/snes-smoke/build.py
	@python3 testdata/public-roms/arcade-smoke/build.py
	@python3 testdata/public-roms/rpgmaker-smoke/build.py

public-fixtures-check:
	@python3 testdata/public-roms/gba-smoke/build.py --check
	@python3 testdata/public-roms/nes-smoke/build.py --check
	@python3 testdata/public-roms/snes-smoke/build.py --check
	@python3 testdata/public-roms/arcade-smoke/build.py --check
	@python3 testdata/public-roms/rpgmaker-smoke/build.py --check

web-e2e: prepare-go prepare-e2e-browser public-fixtures-check
	@PATH="$(NODE_HOME)/bin:$$PATH" scripts/acceptance/web-e2e.sh

data-check:
	@python3 scripts/test_local_user.py
	@python3 scripts/test_pfb.py
	@python3 scripts/test_prepare_toolchains.py
	@python3 scripts/test_makefile.py
	@python3 scripts/test_workflows.py
	@python3 scripts/test_design_assets.py
	@python3 scripts/test_public_fixtures.py
	@python3 scripts/test_dependencies.py
	@python3 scripts/test_rpgmaker_release_assets.py
	@python3 scripts/test_fbalpha2012_dat.py
	@python3 scripts/test_runtime_provider_contract.py
	@python3 scripts/test_runtime_providers.py
	@python3 scripts/test_runtime_target_bindings.py
	@python3 scripts/dependencies.py data-check --versions "$(RETROM_DEPENDENCY_VERSIONS)"

runtime-provider-prepare:
	@python3 scripts/runtime_providers.py prepare --lock-root "$(RETROM_PROVIDER_LOCK_ROOT)" --cache-root "$(RETROM_PROVIDER_CACHE_ROOT)" --installed-root "$(RETROM_PROVIDER_INSTALLED_ROOT)" --active-path "$(RETROM_PROVIDER_ACTIVE_PATH)"

runtime-provider-prepare-candidate:
	@test -n "$(RETROM_RUNTIME_PFB_CANDIDATE_ROOT)" || { echo 'RETROM_RUNTIME_PFB_CANDIDATE_ROOT is required' >&2; exit 2; }
	@python3 scripts/runtime_providers.py prepare-candidate --candidate-root "$(RETROM_RUNTIME_PFB_CANDIDATE_ROOT)" --installed-root "$(RETROM_PROVIDER_INSTALLED_ROOT)" --active-path "$(RETROM_PROVIDER_ACTIVE_PATH)"

runtime-provider-check:
	@python3 scripts/runtime_providers.py check --active-path "$(RETROM_PROVIDER_ACTIVE_PATH)" --installed-root "$(RETROM_PROVIDER_INSTALLED_ROOT)" --source "$(RETROM_PROVIDER_SOURCE)"

runtime-provider-pin-release:
	@test -n "$(RETROM_PROVIDER_RELEASE_ROOT)" || { echo 'RETROM_PROVIDER_RELEASE_ROOT is required' >&2; exit 2; }
	@python3 scripts/runtime_providers.py pin-release --release-root "$(RETROM_PROVIDER_RELEASE_ROOT)" --lock-root "$(RETROM_PROVIDER_LOCK_ROOT)"

runtime-provider-verify-upgrade:
	@test -n "$(RETROM_PROVIDER_CURRENT)" -a -n "$(RETROM_PROVIDER_CANDIDATE)" -a -n "$(RETROM_PROVIDER_CHECKPOINT_REFERENCES)" || { echo 'RETROM_PROVIDER_CURRENT, RETROM_PROVIDER_CANDIDATE and RETROM_PROVIDER_CHECKPOINT_REFERENCES are required' >&2; exit 2; }
	@python3 scripts/runtime_providers.py verify-upgrade --current "$(RETROM_PROVIDER_CURRENT)" --candidate "$(RETROM_PROVIDER_CANDIDATE)" --checkpoint-references "$(RETROM_PROVIDER_CHECKPOINT_REFERENCES)"

prepare-deps:
	@RETROM_MODE="$(RETROM_MODE)" RETROM_RUNTIME_DEV_ROOT="$(RETROM_RUNTIME_DEV_ROOT)" python3 scripts/dependencies.py prepare --versions "$(RETROM_DEPENDENCY_VERSIONS)"

deps-check:
	@RETROM_MODE="$(RETROM_MODE)" RETROM_RUNTIME_DEV_ROOT="$(RETROM_RUNTIME_DEV_ROOT)" python3 scripts/dependencies.py deps-check --versions "$(RETROM_DEPENDENCY_VERSIONS)"

retrom-runtime-dev-link: prepare-node
	@test -n "$(RETROM_RUNTIME_DEV_ROOT)" || { echo 'RETROM_RUNTIME_DEV_ROOT is required' >&2; exit 2; }
	@test "$(RETROM_RUNTIME_DEV_INCLUDE_ASSETS)" = "false" || test "$(RETROM_RUNTIME_DEV_INCLUDE_ASSETS)" = "true" || { echo 'RETROM_RUNTIME_DEV_INCLUDE_ASSETS must be true or false' >&2; exit 2; }
	@if [[ -n "$(RETROM_RUNTIME_PFB_CANDIDATE_ROOT)" ]]; then \
		test "$(RETROM_RUNTIME_DEV_INCLUDE_ASSETS)" = "true"; \
	elif [[ "$(RETROM_RUNTIME_DEV_INCLUDE_ASSETS)" = "true" ]]; then \
		cd "$(RETROM_RUNTIME_DEV_ROOT)" && PATH="$(NODE_HOME)/bin:$$PATH" npm run release:build && PATH="$(NODE_HOME)/bin:$$PATH" npm run package:check; \
	else \
		cd "$(RETROM_RUNTIME_DEV_ROOT)" && PATH="$(NODE_HOME)/bin:$$PATH" npm run build && PATH="$(NODE_HOME)/bin:$$PATH" npm run package:check; \
	fi
	@args=(); if [[ "$(RETROM_RUNTIME_DEV_INCLUDE_ASSETS)" = "true" ]]; then args+=(--include-runtime-assets); fi; if [[ -n "$(RETROM_RUNTIME_PFB_CANDIDATE_ROOT)" ]]; then args+=(--candidate-root "$(RETROM_RUNTIME_PFB_CANDIDATE_ROOT)"); fi; python3 scripts/retrom_runtime_dev.py activate \
		--source "$(RETROM_RUNTIME_DEV_ROOT)" \
		--runtime-root "$(abspath $(RETROM_RUNTIME_MATERIALIZATION_ROOT))" \
		--web-package "$(abspath web/node_modules/@xxxsen/retrom-runtime)" \
		--manifest "$(abspath data/dat/rpgmaker/v1/manifest.json)" "$${args[@]}"

retrom-runtime-dev-unlink:
	@python3 scripts/retrom_runtime_dev.py deactivate \
		--runtime-root "$(abspath $(RETROM_RUNTIME_MATERIALIZATION_ROOT))" \
		--web-package "$(abspath web/node_modules/@xxxsen/retrom-runtime)" \
		--manifest "$(abspath data/dat/rpgmaker/v1/manifest.json)"
	@RETROM_RUNTIME_DEV_ROOT= python3 data/dat/rpgmaker/v1/build.py prepare
	@$(MAKE) web-install RETROM_RUNTIME_DEV_ROOT=

release-input-digest:
	@python3 scripts/release-input-digest.py --versions "$(RETROM_DEPENDENCY_VERSIONS)" --active "$(RETROM_ACTIVE_EMULATORJS_VERSION)"

ci: quality-structure-check api-check backend-check web-check integration-test data-check

require-local-user:
	@python3 scripts/local_user.py

dev: require-local-user prepare-go api-generate-go web-install
	@if [[ -n "$(RETROM_RUNTIME_DEV_ROOT)" ]]; then $(MAKE) retrom-runtime-dev-link RETROM_RUNTIME_DEV_ROOT="$(RETROM_RUNTIME_DEV_ROOT)" RETROM_RUNTIME_DEV_INCLUDE_ASSETS="$(RETROM_RUNTIME_DEV_INCLUDE_ASSETS)" RETROM_RUNTIME_PFB_CANDIDATE_ROOT="$(RETROM_RUNTIME_PFB_CANDIDATE_ROOT)"; fi
	@$(MAKE) prepare-deps RETROM_RUNTIME_DEV_ROOT="$(RETROM_RUNTIME_DEV_ROOT)"
	@RETROM_DEV_STATE_DIR="$(RETROM_DEV_STATE_DIR)" \
	 RETROM_HTTP_ADDR="$(RETROM_HTTP_ADDR)" \
	 RETROM_PUBLIC_ORIGIN="$(RETROM_PUBLIC_ORIGIN)" \
	 RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN="$(RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN)" \
	 RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE="$(RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE)" \
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
	 NEXT_DIST_DIR="$(NEXT_DEV_DIST_DIR)" \
	 PATH="$(NODE_HOME)/bin:$$PATH" env -u RETROM_DEV_CONFIG -u RETROM_RUNTIME_DEV_ROOT -u RETROM_RUNTIME_DEV_INCLUDE_ASSETS -u RETROM_RUNTIME_DEV_RELEASE_OVERRIDES -u RETROM_RUNTIME_MATERIALIZATION_ROOT -u RETROM_RUNTIME_PFB_CANDIDATE_ROOT scripts/dev.sh

pfb-init: require-local-user
	@test -n "$(PFB)" || { echo 'PFB is required' >&2; exit 2; }
	@args=(init --root "$(CURDIR)" --pfb "$(PFB)"); \
	 if [[ -n "$(RUNTIME_ROOT)" ]]; then args+=(--runtime-root "$(RUNTIME_ROOT)"); fi; \
	 if [[ -n "$(CORE_ROOTS)" ]]; then args+=(--core-roots '$(CORE_ROOTS)'); fi; \
	 python3 -m scripts.pfb.cli "$${args[@]}"

pfb-validate pfb-build pfb-use pfb-restart pfb-down pfb-verify: require-local-user
	@test -n "$(PFB)" || { echo 'PFB is required' >&2; exit 2; }
	@python3 -m scripts.pfb.cli "$(@:pfb-%=%)" --root "$(CURDIR)" --pfb "$(PFB)"

pfb-up: require-local-user
	@test -n "$(PFB)" || { echo 'PFB is required' >&2; exit 2; }
	@test "$(or $(PFB_SELECT),true)" = true || test "$(PFB_SELECT)" = false || { echo 'PFB_SELECT must be true or false' >&2; exit 2; }
	@python3 -m scripts.pfb.cli up --root "$(CURDIR)" --pfb "$(PFB)" --select "$(or $(PFB_SELECT),true)"

pfb-status: require-local-user
	@test -n "$(PFB)" || { echo 'PFB is required' >&2; exit 2; }
	@python3 -m scripts.pfb.cli status --root "$(CURDIR)" --pfb "$(PFB)" --format "$(or $(FORMAT),text)"

pfb-logs: require-local-user
	@test -n "$(PFB)" || { echo 'PFB is required' >&2; exit 2; }
	@python3 -m scripts.pfb.cli logs --root "$(CURDIR)" --pfb "$(PFB)" --service "$(or $(SERVICE),all)"

pfb-prune: require-local-user
	@test -n "$(PFB)" && test -n "$(KEEP)" && test -n "$(CONFIRM)" || { echo 'PFB, KEEP and CONFIRM are required' >&2; exit 2; }
	@python3 -m scripts.pfb.cli prune --root "$(CURDIR)" --pfb "$(PFB)" --keep "$(KEEP)" --confirm "$(CONFIRM)"

pfb-destroy: require-local-user
	@test -n "$(PFB)" && test -n "$(CONFIRM)" || { echo 'PFB and CONFIRM are required' >&2; exit 2; }
	@python3 -m scripts.pfb.cli destroy --root "$(CURDIR)" --pfb "$(PFB)" --confirm "$(CONFIRM)"

pfb-gateway-up pfb-gateway-down: require-local-user
	@python3 -m scripts.pfb.cli "$(@:pfb-%=%)" --root "$(CURDIR)"

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

acceptance-case: prepare-go
	@test -n "$(CASE)" || { echo 'CASE is required' >&2; exit 2; }
	@scripts/acceptance/run.sh case "$(CASE)"

acceptance-report:
	@scripts/acceptance/run.sh report
