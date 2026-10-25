.DEFAULT_GOAL := build

FRONTEND_VERSION := v1.1.0
FRONTEND_URL := https://github.com/footprint-dev/footprint-frontend/releases/download/$(FRONTEND_VERSION)/footprint-frontend.tar.gz

.PHONY: prepare frontend generate build debug test

prepare: frontend generate

frontend:
	sh scripts/prepare-frontend.sh "$(FRONTEND_URL)"

generate:
	go generate ./ent

build: prepare
	go build -o bin/footprint ./cmd/footprint

debug:
	go build -o bin/footprint ./cmd/footprint

test: prepare
	go test -race ./...
	go vet ./...
