#!/bin/sh
# Create tests/world/NAME as a module that depends on MODULE@VERSION, with the dependency vendored.
# Usage: tests/world/add.sh NAME MODULE VERSION [MORE_MODULE@VERSION...]
# Then write tests/world/NAME/main.go and run: tests/world/add.sh NAME --vendor
# This is the only step that uses the network (go get).
set -e
cd "$(dirname "$0")"
name=$1
if [ "$2" = "--vendor" ]; then
	cd "$name"
	go mod tidy
	go mod vendor
	echo "vendored $name"
	exit 0
fi
mod=$2
ver=$3
shift 3
mkdir -p "$name"
cd "$name"
[ -f go.mod ] || go mod init "marmotworld/$name"
go get "$mod@$ver" "$@"
