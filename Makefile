# Roost build and development tasks. Run `make help` for the list.

GO      ?= go
BUN     ?= bun
# The port `make dev` runs Vite on. The Go server proxies the web UI to it.
VITE_PORT ?= 5173
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help
.PHONY: help build web dev dev-api dev-web test lint types clean

## build: build ./roost with the web UI embedded
build: web
	$(GO) build -tags ui -trimpath -ldflags '$(LDFLAGS)' -o roost ./cmd/roost

## web: build the web UI into web/build
web: web/node_modules
	cd web && $(BUN) run build

## dev: run the Go server and the Vite dev server together
dev: web/node_modules
	@$(MAKE) --no-print-directory -j2 dev-api dev-web

dev-api:
	$(GO) run ./cmd/roost serve --dev-ui http://127.0.0.1:$(VITE_PORT)

dev-web:
	cd web && $(BUN) run dev --port $(VITE_PORT)

## test: run the Go and web tests, and type-check the web app
test: web/node_modules
	$(GO) test -race -shuffle=on ./...
	cd web && $(BUN) run test
	cd web && $(BUN) run check

## lint: run golangci-lint, shellcheck and prettier
lint: web/node_modules
	golangci-lint run ./...
	shellcheck agent/*.sh install.sh .github/scripts/*.sh
	cd web && $(BUN) run lint

## types: regenerate web/src/lib/api/types.ts from internal/api/types.go
types:
	$(GO) tool tygo generate

## clean: remove build output
clean:
	rm -rf roost web/build web/.svelte-kit

web/node_modules: web/package.json web/bun.lock
	cd web && $(BUN) install --frozen-lockfile
	@touch $@

## help: list these targets
help:
	@awk '/^## / { sub(/^## /, ""); split($$0, a, ": "); printf "  %-7s %s\n", a[1], a[2] }' $(MAKEFILE_LIST)
