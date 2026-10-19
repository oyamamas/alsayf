GO ?= go
CGO_ENABLED ?= 0
export CGO_ENABLED

.PHONY: all core full

all: full

core:
	mkdir -p bin
	$(GO) build -trimpath -ldflags="-s -w" -tags core -o bin/alsayf-core .

full:
	mkdir -p bin
	$(GO) build -trimpath -ldflags="-s -w" -o bin/alsayf .
