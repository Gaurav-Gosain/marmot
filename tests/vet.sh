#!/bin/sh
# Run go vet on each test file separately (each is its own package main).
# Usage: tests/vet.sh [dir...]   default: tests/run
cd "$(dirname "$0")" || exit 1
status=0
for f in $(find ${@:-run} -name '*.go' ! -name '*_badfmt.go' | sort); do
	out=$(go vet "$f" 2>&1 | grep -v '^#')
	if [ -n "$out" ]; then
		echo "$out"
		status=1
	fi
done
exit $status
