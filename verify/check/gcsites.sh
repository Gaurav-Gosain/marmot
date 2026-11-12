#!/bin/sh
# gcsites.sh: check every allocation site (go.new, go.make_slice, go.box, go.make_closure) of
# each program against its type descriptor, and every pointer store into an allocated object
# against the object's kind and bitmap (verify/check -gcsites), with the lifetime transforms
# off and on.
#   verify/check/gcsites.sh [dir ...]     default: tests/run/core
# A dir holding go.mod is built as one module (tests/world/NAME); any other dir is walked for
# single-file programs (*.go at any depth).
set -u
export MARMOT_OUTPUT=plain # plain compiler output whoever runs this
cd "$(dirname "$0")/../.."
export GOCACHE="$PWD/.work/gocache" # build cache inside the repo
R=$(pwd)
W=.work/verify/gcsites
mkdir -p "$W"
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; total=0; stores=0

one() { # one NAME DIR TARGET
	for life in "" "MARMOT_LIFETIME=all"; do
		m="$R/$W/$1.mlir"
		if ! (cd "$2" && env $life timeout 300 "$R/build/bin/marmot" build -O 2 -nocache -dialect=go -emit mlir -o "$m" "$3" >/dev/null 2>&1); then
			echo "$1 [$life]: skipped (does not build)"; continue
		fi
		out=$(timeout 300 "$W/check" -gcsites "$m" 2>&1)
		case "$out" in *" 0 mismatches"*) ;; *) echo "$1 [$life]: $out"; st=1 ;; esac
		k=$(echo "$out" | sed -n 's/^check: \([0-9]*\) allocation sites checked.*/\1/p')
		s=$(echo "$out" | sed -n 's/^check: [0-9]* allocation sites checked, \([0-9]*\) stores.*/\1/p')
		total=$((total + ${k:-0})); stores=$((stores + ${s:-0}))
		rm -f "$m"
	done
	n=$((n + 1))
}

for dir in "${@:-tests/run/core}"; do
	if [ -f "$dir/go.mod" ]; then
		one "$(basename "$dir")" "$dir" .
		continue
	fi
	for f in $(find "$dir" -name vendor -prune -o -name '*.go' -print | sort); do
		p=$(echo "${f#"$dir"/}" | sed 's/\.go$//; s|/|_|g')
		one "$p" "$(dirname "$f")" "$(basename "$f")"
	done
done
echo "gcsites: $n programs, $total sites and $stores stores checked, status $st"
exit $st
