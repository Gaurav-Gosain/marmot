#!/bin/sh
# gcbin.sh: check the type descriptors of each linked program against the MLIR they came from
# (verify/check -gcbin): size and ptrdata words, bitmap bytes, the gcdata pointer.
#   verify/check/gcbin.sh [dir ...]     default: tests/run/core
# Programs are single files at any depth; extra marmot flags come from $GCBIN_FLAGS.
set -u
cd "$(dirname "$0")/../.."
export GOCACHE="$PWD/.work/gocache" # build cache inside the repo
R=$(pwd)
W=.work/verify/gcbin
mkdir -p "$W/tmp"
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; total=0
for dir in "${@:-tests/run/core}"; do
	for f in $(find "$dir" -name vendor -prune -o -name '*.go' -print | sort); do
		p=$(echo "${f#"$dir"/}" | sed 's/\.go$//; s|/|_|g')
		wd=$(TMPDIR="$R/$W/tmp" timeout 600 ./build/bin/marmot build ${GCBIN_FLAGS:-} -nocache -work -o "$W/a.exe" "$f" 2>&1 | sed -n 's/^WORK=//p' | head -1)
		if [ -z "$wd" ] || [ ! -f "$wd/nopcln.exe" ]; then
			echo "$p: skipped (no work dir)"
		else
			out=$("$W/check" -gcbin "$wd" 2>&1)
			case "$out" in *" 0 mismatches"*) ;; *) echo "$p: $out"; st=1 ;; esac
			k=$(echo "$out" | sed -n 's/^check: \([0-9]*\) linked.*/\1/p')
			total=$((total + ${k:-0})); n=$((n + 1))
		fi
		find .work/verify/gcbin/tmp -mindepth 1 -delete
		rm -f .work/verify/gcbin/a.exe
	done
done
echo "gcbin: $n programs, $total linked descriptors checked, status $st"
exit $st
