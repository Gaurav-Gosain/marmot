# Top-level build. Each component owns its own build script; this file only dispatches.
ROOT := $(abspath .)
export GO2MLIR_ROOT := $(ROOT)

.PHONY: all frontend backend rtcore test conform bench clean

all: frontend backend rtcore

frontend:
	go build -o build/bin/go2mlir ./cmd/go2mlir

backend:
	./backend/build.sh

rtcore:
	$(MAKE) -C runtime/core

test: all
	go test ./...
	$(MAKE) conform

conform: all
	go run ./tests/conform

bench: all
	./bench/run.sh

clean:
	rm -rf build
