# Top-level build. Each component owns its own build script; this file only dispatches.
ROOT := $(abspath .)
export MARMOT_ROOT := $(ROOT)

.PHONY: diagrams all frontend backend rtcore test conform bench clean

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

clean:
	rm -rf build

# Render docs/diagrams/*.mmd to light and dark SVG with sketch (go install tuios.dev/sketch/cmd/sketch@latest).
diagrams:
	@for f in docs/diagrams/*.mmd; do n=$$(basename $$f .mmd); \
	  sketch $$f -theme flexoki -o docs/img/$$n-light.svg; \
	  sketch $$f -theme flexoki-dark -o docs/img/$$n-dark.svg; done
