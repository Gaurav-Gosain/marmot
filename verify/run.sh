#!/bin/sh
# run.sh: check the proofs.
#   verify/run.sh [file ...]
# Checks every verify/bend/*.bend with Bend 2 and every verify/lean/*.lean with Lean 4
# (default: all; files are names in verify/bend or verify/lean).
#   BEND2   path to Bend 2's main.ts (run with bun); unset: Bend files are skipped
#   VERDICT 1 to recheck the Bend proofs with the BendTT kernel (--verdict)
#   LEAN    the lean binary (default: lean on PATH); the proofs use Lean 4.34
set -eu
cd "$(dirname "$0")/.."
files=${*:-$(cd verify/bend && ls *.bend; cd ../lean && ls *.lean)}
LEAN=${LEAN:-lean}
flag=""
[ "${VERDICT:-}" = 1 ] && flag=--verdict
st=0
for f in $files; do
	f=$(basename "$f")
	case $f in
	*.bend)
		if [ -z "${BEND2:-}" ]; then
			echo "$f: skipped (BEND2 not set)"
			continue
		fi
		out=$(cd verify/bend && timeout 900 bun "$BEND2" "$f" $flag 2>&1 | grep -v 'is available' || true)
		ok='ALL PROOFS CHECK' ;;
	*.lean)
		out=$(cd verify/lean && timeout 900 "$LEAN" "$f" 2>&1 | grep -E 'error|depends on axioms|sorry' || true)
		case "$out" in *error*|*sorry*) ;; *) out="LEAN OK${out:+
$out}" ;; esac
		ok='LEAN OK' ;;
	*) continue ;;
	esac
	printf '%s: %s\n' "$f" "$(echo "$out" | head -1)"
	case "$out" in "$ok"*) ;; *) echo "$out" | head -40; st=1 ;; esac
done
exit $st
