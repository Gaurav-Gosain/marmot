#!/bin/sh
# Benchmark entry point. Builds nothing and never installs into build/: it
# snapshots whatever is installed in build/.
# Usage: bench/run.sh [-programs a,b] [-toolchains go,tinygo-O2] [-label name] [-runs N] ...
# See `bench/run.sh -h` for all options.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
exec go run ./bench/cmd/bench "$@"
