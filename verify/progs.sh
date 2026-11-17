#!/bin/sh
# progs.sh: run the programs generated from the proofs' side conditions
# (verify/testdata/progs/*.go, expected output from stock Go in *.out) under marmot in
# three configurations: dev (llvm dialect), go dialect (go-bce and the other go passes),
# go dialect with the loop passes (-par), every lifetime transform on (MARMOT_LIFETIME=all), and the
# stack buffer for append from nil (MARMOT_APPENDSTACK=1, go dialect). PROGS_CONFIGS picks the
# configurations; also esc (the three escape analysis switches).
# Each binary also runs under MARMOT_POISON=1 MARMOT_GCSTRESS=1 (freed memory poisoned and
# trapping, a collection at every allocation). Exit 1 on any difference.
# Programs listed in verify/testdata/progs/KNOWN_OPEN are counterexamples whose fix is pending;
# their failures are reported but do not fail the run.
set -u
export MARMOT_OUTPUT=plain # plain compiler output whoever runs this
cd "$(dirname "$0")/.."
export GOCACHE="$PWD/.work/gocache" # build cache inside the repo
W=.work/verify/progs
mkdir -p "$W"
st=0
for f in verify/testdata/progs/*.go; do
	p=$(basename "$f" .go)
	open=0
	grep -qx "$p" verify/testdata/progs/KNOWN_OPEN 2>/dev/null && open=1
	for cfg in ${PROGS_CONFIGS:-dev go par life astk}; do
		fl=""; bf=""; le=""
		[ "$cfg" != dev ] && fl="-dialect=go"
		[ "$cfg" = par ] && bf="-par"
		[ "$cfg" = life ] && le="MARMOT_LIFETIME=all"
		[ "$cfg" = astk ] && le="MARMOT_APPENDSTACK=1"
		[ "$cfg" = esc ] && le="MARMOT_XPKG_ESCAPE=1 MARMOT_ESCAPE_SUMMARY=1 MARMOT_ESCAPE_EXT=1"
		x="$W/$p.$cfg"
		if ! env $le MARMOT_BACKEND_FLAGS="$bf" timeout 300 ./build/bin/marmot build -O 2 -nocache $fl -o "$x" "$f" >/dev/null 2>&1; then
			echo "$p $cfg: build failed"; st=1; continue
		fi
		for run in plain poison; do
			re=""
			[ "$run" = poison ] && re="MARMOT_POISON=1 MARMOT_GCSTRESS=1"
			if env $re timeout 120 "$x" 2>&1 | cmp -s - "verify/testdata/progs/$p.out"; then
				echo "$p $cfg $run: ok"
			elif [ $open = 1 ]; then
				echo "$p $cfg $run: DIFFERS (known open counterexample)"
			else
				echo "$p $cfg $run: DIFFERS from Go"; st=1
			fi
		done
		rm -f "$x"
	done
done
exit $st
