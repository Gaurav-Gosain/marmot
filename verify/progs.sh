#!/bin/sh
# progs.sh: run the programs generated from the proofs' side conditions
# (verify/testdata/progs/*.go, expected output from stock Go in *.out) under marmot in
# three configurations: dev (llvm dialect), go dialect (go-bce and the other go passes),
# and go dialect with the loop passes (-par). Exit 1 on any difference.
set -u
cd "$(dirname "$0")/.."
W=.work/verify/progs
mkdir -p "$W"
st=0
for f in verify/testdata/progs/*.go; do
	p=$(basename "$f" .go)
	for cfg in dev go par; do
		fl=""; bf=""
		[ "$cfg" != dev ] && fl="-dialect=go"
		[ "$cfg" = par ] && bf="-par"
		x="$W/$p.$cfg"
		if ! MARMOT_BACKEND_FLAGS="$bf" timeout 300 ./build/bin/marmot build -O 2 -nocache $fl -o "$x" "$f" >/dev/null 2>&1; then
			echo "$p $cfg: build failed"; st=1; continue
		fi
		if timeout 60 "$x" 2>&1 | cmp -s - "verify/testdata/progs/$p.out"; then
			echo "$p $cfg: ok"
		else
			echo "$p $cfg: DIFFERS from Go"; st=1
		fi
		rm -f "$x"
	done
done
exit $st
