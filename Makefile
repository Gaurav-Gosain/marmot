# Top-level build. Each component owns its own build script; this file only dispatches.
ROOT := $(abspath .)
export MARMOT_ROOT := $(ROOT)

.PHONY: install diagrams all frontend check-tool backend rtcore test conform bench clean

all: frontend check-tool backend rtcore

# GOEXPERIMENT=nogreenteagc: with the Green Tea collector (default since Go
# 1.26) code generation sometimes spends most of its CPU time in the
# collector's span scanning (tryDeferToSpanScan) when every core generates a
# package; the classic collector is steady. Temporary, until that is fixed
# upstream or the experiment goes away.
frontend:
	GOEXPERIMENT=nogreenteagc go build -o build/bin/marmot ./cmd/marmot

# The analyzers of marmot check, a separate binary that marmot runs.
check-tool:
	go build -o build/bin/marmot-check ./cmd/marmot-check

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
