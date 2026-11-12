#!/bin/sh
# Build the linux runtime libraries on a Linux machine with LLVM 21 or older
# (its bitcode must load in LLVM 22) and run the tests there. Run from
# runtime/core in a copy of the tree; LLVM selects the toolchain prefix.
# Results: build/lib/linux-amd64 and build/lib/linux-arm64 of that copy.
set -e
LLVM=${LLVM:-/usr/lib/llvm21}
for a in amd64 arm64; do timeout 300 make -s GOOS=linux GOARCH=$a LLVM=$LLVM; done
ls -la ../../build/lib/linux-*/
timeout 900 make -s test LLVM=$LLVM 2>&1 | tail -3
