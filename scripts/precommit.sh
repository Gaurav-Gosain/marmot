#!/bin/sh
# precommit.sh: check what is staged for the next commit with marmot check -staged.
#
#   scripts/precommit.sh                     run by hand before committing
#   ln -s ../../scripts/precommit.sh .git/hooks/pre-commit    opt in as a git hook
#
# It checks the Go code of the compiler (cmd and internal) as git has staged it, not the working
# tree, with the repository's .golangci.yml: fmt, vet, lint, gopls, tidy and cross fail the
# commit; modernize and nil only report. The vulnerability check needs the network and is
# skipped; dead code is left out because it loads the whole module, test corpus included (some of
# its programs do not build on purpose). Results are cached per package: once the caches are
# warm, a commit that touches a few files is checked in about a second. Environment: MARMOT
# (the marmot binary; default build/bin/marmot, else marmot on PATH), PRECOMMIT_ONLY (checks to
# run, comma separated), PRECOMMIT_PACKAGES (patterns).
# git commit --no-verify skips the hook.
set -eu
ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"
M=${MARMOT:-}
if [ -z "$M" ]; then
	if [ -x build/bin/marmot ]; then
		M=build/bin/marmot
	elif command -v marmot >/dev/null 2>&1; then
		M=marmot
	else
		echo "precommit: no marmot (build it with make, or set MARMOT)" >&2
		exit 1
	fi
fi
ONLY=${PRECOMMIT_ONLY:-fmt,vet,lint,modernize,gopls,nil,tidy,cross}
# shellcheck disable=SC2086 # the patterns are words
exec "$M" check -staged -offline -only "$ONLY" ${PRECOMMIT_PACKAGES:-./cmd/... ./internal/...}
