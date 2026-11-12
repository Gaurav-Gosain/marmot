#!/bin/sh
# Benchmark entry point. Builds nothing and never installs into build/ (SPEC rule 12): it
# snapshots whatever frontend, backend and rtcore have installed in build/.
# Usage: bench/run.sh [-programs a,b] [-toolchains go,tinygo-O2] [-label name] [-runs N] ...
# See `bench/run.sh -h` for all options.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
exec go run ./bench/cmd/bench "$@"
