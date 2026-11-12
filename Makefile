# Top-level build. Each component owns its own build script; this file only dispatches.
ROOT := $(abspath .)
export MARMOT_ROOT := $(ROOT)

.PHONY: install diagrams all frontend backend rtcore test conform bench clean

all: frontend backend rtcore

frontend:
	go build -o build/bin/marmot ./cmd/marmot

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

# Copy the built compiler to ~/.marmot and link it into ~/.local/bin (PREFIX, BINDIR override).
install:
	./scripts/install.sh

clean:
	rm -rf build

# Render docs/diagrams/*.mmd to light and dark SVG with sketch.
diagrams:
	./scripts/diagrams.sh
