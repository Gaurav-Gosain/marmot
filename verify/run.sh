#!/bin/sh
# run.sh: check the proofs.
#   verify/run.sh [file ...]      on linux-host (the record): every verify/bend/*.bend with bend2
#                                 and then the BendTT kernel (--verdict), and every
#                                 verify/lean/*.lean with Lean 4.34.0
#   verify/run.sh -l [file ...]   quick local check: bend2 only (no kernel recheck), from a copy
#                                 in .work/verify/tools/bend, and Lean from ~/.elan if present
# Files are names in verify/bend or verify/lean (default: all).
set -eu
cd "$(dirname "$0")/.."
files=${*:-}
if [ "${1:-}" = -l ]; then
	shift
	files=${*:-$(cd verify/bend && ls *.bend; cd ../lean && ls *.lean)}
	B=$PWD/.work/verify/tools/bend/bend2/main.ts
	[ -f "$B" ] || scripts/linux/remote.sh get marmot/verify-tools/bend .work/verify/tools/ >/dev/null
	LEAN=$(ls -d "$HOME"/.elan/toolchains/leanprover--lean4---v4.34.*/bin/lean 2>/dev/null | tail -1)
	st=0
	for f in $files; do
		f=$(basename "$f")
		case $f in
		*.bend) out=$(cd verify/bend && timeout 120 bun "$B" "$f" 2>&1 | grep -v 'is available' || true)
			ok='ALL PROOFS CHECK' ;;
		*.lean) out=$(cd verify/lean && timeout 300 "$LEAN" "$f" 2>&1 | grep -E 'error|depends on axioms|sorry' || true)
			case "$out" in *error*|*sorry*) ;; *) out="LEAN OK${out:+
$out}" ;; esac
			ok='LEAN OK' ;;
		esac
		printf '%s: %s\n' "$f" "$(echo "$out" | head -1)"
		case "$out" in "$ok"*) ;; *) echo "$out" | head -40; st=1 ;; esac
	done
	exit $st
fi
files=${files:-$(cd verify/bend && ls *.bend; cd ../lean && ls *.lean)}
list=""
for f in $files; do list="$list $(basename "$f")"; done
R=scripts/linux/remote.sh
$R put verify/bend verify/lean marmot/verify-tools/work/ >/dev/null
$R run -n verify-proofs -t 3600 -m 4G -c 2 -d marmot/verify-tools/work -- \
	"st=0; L=\$HOME/.elan/toolchains/leanprover--lean4---v4.34.0/bin/lean; for f in $list; do printf '%s: ' \$f; case \$f in *.bend) out=\$(cd bend && timeout 900 bun ../../bend/bend2/main.ts \$f --verdict 2>&1 | grep -v 'is available'); ok='ALL PROOFS CHECK';; *.lean) out=\$(cd lean && timeout 900 \$L \$f 2>&1 | grep -E 'error|sorry|depends on axioms'); case \"\$out\" in *error*|*sorry*) ;; *) out=\"LEAN OK (\$(\$L --version | head -1)) \$out\";; esac; ok='LEAN OK';; esac; echo \"\$out\" | head -1; case \"\$out\" in \"\$ok\"*) ;; *) echo \"\$out\" | head -30; st=1;; esac; done; exit \$st"
