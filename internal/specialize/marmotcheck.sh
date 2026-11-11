#!/bin/bash
# marmotcheck.sh [-O LEVEL]: run every differential test program of this package under marmot
# with MARMOT_SPEC=all as well as under stock Go, and compare output and exit status. Covers the
# paths that only fire against marmot's runtime (mapkey, regexp hook, json entry points).
# Default -release; the 9000-site fmt program needs -O 2 (backend time limit in -release).
set -u
cd "$(dirname "$0")/../.."
R=$PWD; O=${2:--release}; [ "${1:-}" = -O ] && O="-O $2"
D=$R/.work/spec/progs; rm -rf "$D"; mkdir -p "$D"
SPEC_TEST_PROGRAMS=$D go test ./internal/specialize/ >/dev/null || { echo "go test failed"; exit 1; }
go build -o "$D/marmot" ./cmd/marmot || exit 1
st=0
for d in "$D"/*/; do
	n=$(basename "$d")
	(cd "$d" && printf 'module p\ngo 1.27\n' > go.mod && go run . > ../$n.go.out 2>&1; echo "exit $?" >> ../$n.go.out; rm go.mod)
	o="$O"; [ "$n" = fmtdifferential ] && o="-O 2"
	if ! MARMOT_ROOT=$R MARMOT_SPEC=all nice timeout 1800 "$D/marmot" build -v $o -o "$D/$n.exe" "$d" > "$D/$n.log" 2>&1; then
		echo "$n BUILDFAIL"; st=1; continue
	fi
	(cd "$d" && timeout 120 ../$n.exe > ../$n.mm.out 2>&1; echo "exit $?" >> ../$n.mm.out)
	if cmp -s "$D/$n.go.out" "$D/$n.mm.out"; then r=SAME; else r=DIFF; st=1; fi
	echo "$n $r $(grep -o 'specialize: .*' "$D/$n.log")"
	rm -f "$D/$n.exe"
done
exit $st
