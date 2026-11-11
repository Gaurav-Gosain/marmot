#!/bin/sh
# corpus.sh: re-check go-bce on real compiler output for every program in the given
# directories (default tests/run/core).
#   verify/check/corpus.sh [dir ...]
# For each program: marmot emits go dialect MLIR, marmot-opt runs go-bce with its report
# (the certificate), and verify/check validates every claimed removal and that the report
# names exactly the checks that disappeared. Exit 1 if any removal is not justified.
set -u
cd "$(dirname "$0")/../.."
W=.work/verify/corpus
mkdir -p "$W"
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; rem=0
for dir in "${@:-tests/run/core}"; do
	for f in "$dir"/*.go; do
		p=$(basename "$f" .go)
		b="$W/$p.mlir"; a="$W/$p.bce.mlir"; r="$W/$p.report"
		rm -f "$b" "$a" "$r"
		if ! timeout 120 ./build/bin/marmot build -O 2 -nocache -dialect=go -emit mlir -o "$b" "$f" >/dev/null 2>&1; then
			echo "$p: skipped (does not build)"; continue
		fi
		timeout 60 ./build/bin/marmot-opt --go-bce="report=$r" "$b" > "$a" 2>/dev/null || { echo "$p: marmot-opt failed"; st=1; continue; }
		touch "$r"
		out=$(timeout 60 "$W/check" -report "$r" -before "$b" -after "$a" 2>&1)
		case "$out" in *" 0 without justification") ;; *) echo "$p: $out"; st=1 ;; esac
		k=$(wc -l < "$r"); rem=$((rem + k)); n=$((n + 1))
		rm -f "$b" "$a" "$r"
	done
done
echo "corpus: $n programs, $rem removals checked, status $st"
exit $st
