.PHONY: build

build:
	go generate ./ent
	go build -o bin/footprint ./cmd/footprint
