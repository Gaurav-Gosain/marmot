#!/bin/sh
# Copy what the compiler needs into .work/tests/tree-fuzz-PID and print the path, so long runs
# (fuzzing) are not disturbed by other workers editing the live tree. Smoke tests the copy.
# Usage: tests/snapshot.sh   then   go run ./tests/fuzz -root "$(tests/snapshot.sh)"
set -e
root=$(cd "$(dirname "$0")/.." && pwd)
export GOCACHE=${GOCACHE:-$root/.work/gocache}
dst="$root/.work/tests/tree-fuzz-$$"
mkdir -p "$dst/build"
cp -R "$root/build/bin" "$root/build/lib" "$dst/build/"
cp -R "$root/lib" "$dst/lib"
printf 'package main\n\nfunc main() { println("hello") }\n' > "$dst/hello.go"
MARMOT_CACHE_MAX_MB=512 MARMOT_ROOT="$dst" "$dst/build/bin/marmot" build -o "$dst/hello" "$dst/hello.go" >&2
"$dst/hello" 2>&1 | grep -q hello
echo "$dst"
