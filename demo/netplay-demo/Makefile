SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

NPM := corepack npm
HOST ?= 127.0.0.1
PORT ?= 4174
SMOKE_TARGET ?= 3000
PEGASUS_ROOT ?= $(RETROM_PEGASUS_ROOT)

.PHONY: help install prepare assets unit smoke test check-port start test-and-start

help:
	@echo "Retrom netplay demo"
	@echo
	@echo "  make start           Verify assets and start UI + WebSocket relay"
	@echo "  make test            Run unit, asset, and real-browser tests"
	@echo "  make test-and-start  Run all tests, then start the interactive demo"
	@echo "  make prepare         Materialize pinned runtime/core/ROM assets"
	@echo
	@echo "Defaults: HOST=$(HOST) PORT=$(PORT) SMOKE_TARGET=$(SMOKE_TARGET)"
	@echo "Ports 3000 and 8080 are reserved for the main Retrom applications."
	@echo "Set PUBLIC_ORIGIN=https://example.test when running behind a reverse proxy."

install: node_modules/.package-lock.json

node_modules/.package-lock.json: package.json package-lock.json
	@$(NPM) ci --ignore-scripts --omit=optional

prepare: install
	@if [[ -z "$(PEGASUS_ROOT)" ]]; then \
		echo "Set PEGASUS_ROOT or RETROM_PEGASUS_ROOT before make prepare." >&2; \
		exit 2; \
	fi
	@node tools/prepare-assets.mjs \
		--pegasus-root "$(PEGASUS_ROOT)"

assets:
	@node tools/verify-assets.mjs

unit: install
	@$(NPM) test

smoke: install assets
	@NETPLAY_SMOKE_TARGET="$(SMOKE_TARGET)" $(NPM) run smoke

test: unit assets smoke

check-port:
	@HOST="$(HOST)" PORT="$(PORT)" node tools/serve.mjs --check-config

start: install assets check-port
	@HOST="$(HOST)" PORT="$(PORT)" $(NPM) run serve

test-and-start: test check-port
	@HOST="$(HOST)" PORT="$(PORT)" $(NPM) run serve
