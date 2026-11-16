#!/bin/sh
# gccheck.sh: check the pointer bitmaps of every type descriptor in the MLIR of each program
# (verify/check -gcdata) against gc's layout computed independently from go/types.
#   verify/check/gccheck.sh [dir ...]     default: tests/run/core
# GCCHECK_TARGET=GOOS/GOARCH builds for that target (marmot -target) and checks with its layout,
# for example GCCHECK_TARGET=wasip1/wasm (4-byte pointers, one bitmap bit per 4 bytes).
# A program that does not build fails the run unless it is in GCCHECK_SKIP_OK (names as printed,
# space separated; default for wasip1/wasm: xsys_unix, which needs a Unix target).
# A dir holding go.mod is built as one module (tests/world/NAME); any other dir is walked for
# single-file programs (*.go at any depth, as in tests/run/std/PKG).
set -u
export MARMOT_OUTPUT=plain # plain compiler output whoever runs this
cd "$(dirname "$0")/../.."
export GOCACHE="$PWD/.work/gocache" # build cache inside the repo
R=$(pwd)
W=.work/verify/gccheck
mkdir -p "$W"
tflag= cflags= skips=0
known=
[ "${GCCHECK_TARGET:-}" = wasip1/wasm ] && known=xsys_unix
known=" ${GCCHECK_SKIP_OK:-$known} "
if [ -n "${GCCHECK_TARGET:-}" ]; then
	tflag="-target $GCCHECK_TARGET"
	cflags="-goos ${GCCHECK_TARGET%/*} -goarch ${GCCHECK_TARGET#*/}"
fi
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; total=0

one() { # one NAME DIR TARGET
	m="$R/$W/$1.mlir"
	if ! (cd "$2" && timeout 300 "$R/build/bin/marmot" build $tflag -O 2 -nocache -emit mlir -o "$m" "$3" >/dev/null 2>&1); then
		skips=$((skips + 1))
		case "$known" in
		*" $1 "*) echo "$1: skipped (does not build; known)" ;;
		*) echo "$1: does not build"; st=1 ;;
		esac
		return
	fi
	out=$(cd "$2" && timeout 300 "$R/$W/check" $cflags -gcdata "$m" "$3" 2>&1)
	case "$out" in *" 0 mismatches"*) ;; *) echo "$1: $out"; st=1 ;; esac
	k=$(echo "$out" | sed -n 's/^check: \([0-9]*\) type descriptors checked.*/\1/p')
	s=$(echo "$out" | sed -n 's/.* \([0-9]*\) skipped$/\1/p')
	[ "${s:-0}" != 0 ] && echo "$1: ${s} descriptors skipped"
	total=$((total + ${k:-0})); n=$((n + 1))
	rm -f "$m"
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
# a run that checked nothing fails: no program built (no marmot binary?) or none was found
[ "$n" -gt 0 ] || { echo "gccheck: no program checked"; st=2; }
echo "gccheck${GCCHECK_TARGET:+ $GCCHECK_TARGET}: $n programs, $total descriptors checked, $skips not built, status $st"
exit $st
