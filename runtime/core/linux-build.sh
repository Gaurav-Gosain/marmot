#!/bin/sh
# Build the linux runtime libraries on a Linux host with LLVM 21 or older
# (its bitcode must load in LLVM 22) and run the tests there.
#   ssh HOST 'sh -s' < runtime/core/linux-build.sh    after copying the tree to ~/.cache/marmot/rtcore
# Results: build/lib/linux-amd64 and build/lib/linux-arm64 under that tree.
set -e
cd ~/.cache/marmot/rtcore/runtime/core
LLVM=${LLVM:-/usr/lib/llvm21}
for a in amd64 arm64; do timeout 300 make -s GOOS=linux GOARCH=$a LLVM=$LLVM; done
ls -la ../../build/lib/linux-*/
timeout 900 make -s test LLVM=$LLVM 2>&1 | tail -3
