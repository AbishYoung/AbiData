SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

ABIDATA_BOOTSTRAP_ADMIN_EMAIL ?= admin@example.com

.PHONY: dev dev-web test lint gen build docker docker-local e2e screenshots web-deps web-build clean

web/node_modules: web/package-lock.json
	npm --prefix web ci
	@touch web/node_modules

web-deps:
	npm --prefix web ci
	@touch web/node_modules

web-build: web/node_modules
	npm --prefix web run build

dev: web-build
	ABIDATA_DATA_DIR=$(CURDIR)/.data/data \
	ABIDATA_BACKUP_DIR=$(CURDIR)/.data/backups \
	ABIDATA_PUBLIC_URL=http://localhost:8080 \
	ABIDATA_LISTEN_ADDR=:8080 \
	ABIDATA_BOOTSTRAP_ADMIN_EMAIL=$(ABIDATA_BOOTSTRAP_ADMIN_EMAIL) \
	go run ./cmd/abidata serve

dev-web: web/node_modules
	npm --prefix web run dev

test: web/node_modules
	go test -race ./...
	npm --prefix web test

lint: web/node_modules
	golangci-lint run ./...
	npm --prefix web run typecheck
	npm --prefix web run lint

gen: web/node_modules
	go generate ./...
	npm --prefix web run gen:api
	npm --prefix web run gen:shortcuts

build: web-build
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/abidata ./cmd/abidata

# Multi-platform images cannot be loaded into the local daemon; use docker-local for that.
docker:
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t abidata:dev .

docker-local:
	docker buildx build --load --build-arg VERSION=$(VERSION) -t abidata:dev .

e2e:
	npm --prefix tests/e2e test

# Regenerates docs/images/*. Needs `npm --prefix tests/e2e ci` and `npx playwright install chromium` once.
screenshots:
	node scripts/screenshots.mjs

clean:
	rm -rf bin .data web/node_modules
	find web/dist -mindepth 1 ! -name .gitkeep -delete 2>/dev/null || true
