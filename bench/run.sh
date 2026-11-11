#!/bin/sh
# Benchmark entry point. Builds nothing itself; run `make` first so build/bin/marmot exists.
# Usage: bench/run.sh [-programs a,b] [-toolchains go,tinygo-O2] [-label name] [-runs N] ...
# See `bench/run.sh -h` for all options.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
exec go run ./bench/cmd/bench "$@"
