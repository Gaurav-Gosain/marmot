#!/bin/sh
# run.sh: check the Bend proofs (and the Lean parts, if any) on linux-host.
#   verify/run.sh [file.bend ...]    default: every verify/bend/*.bend
# The checker is Bend 2 from a copy of the bend2go checkout in ~/marmot/verify-tools/bend
# (see verify/README.md for the version). Every file is checked twice: by bend2 and,
# with --verdict, by the BendTT kernel whose soundness is proved in Lean.
set -eu
cd "$(dirname "$0")/.."
R=scripts/linux/remote.sh
$R put verify/bend marmot/verify-tools/work/ >/dev/null
files=${*:-$(cd verify/bend && ls *.bend)}
list=""
for f in $files; do list="$list $(basename "$f")"; done
$R run -n verify-bend -t 1800 -m 4G -c 2 -d marmot/verify-tools/work/bend -- \
	"st=0; for f in $list; do printf '%s: ' \$f; out=\$(timeout 900 bun ../../bend/bend2/main.ts \$f --verdict 2>&1 | grep -v 'is available'); echo \"\$out\" | head -1; case \"\$out\" in 'ALL PROOFS CHECK'*) ;; *) echo \"\$out\" | head -30; st=1;; esac; done; exit \$st"
