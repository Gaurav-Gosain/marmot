#!/bin/sh
# Installs the backend tools into build/bin: only a build of the committed
# backend tree (HEAD) that passed the backend tests. Either the private build
# of a clean working tree (backend/test.sh) or the build of HEAD in its own
# worktree (BACKEND_HEAD=1 backend/test.sh), which works with uncommitted
# changes in this tree.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WANT=$(git -C "$ROOT" rev-parse HEAD:backend)
tested() { [ -f "$1/.tested" ] && [ "$(cat "$1/.tested")" = "$WANT" ]; }
if [ -n "${BACKEND_BIN_DIR:-}" ]; then
  SRC=$BACKEND_BIN_DIR
elif tested "$ROOT/build/backend-head/bin"; then
  SRC=$ROOT/build/backend-head/bin
else
  SRC=$ROOT/build/backend-test/bin
  if ! git -C "$ROOT" diff --quiet HEAD -- backend; then
    echo "install.sh: backend/ has uncommitted changes; commit first or use BACKEND_HEAD=1 backend/test.sh" >&2
    exit 1
  fi
fi
if ! tested "$SRC"; then
  echo "install.sh: no passing test run for this backend tree; run backend/test.sh" >&2
  exit 1
fi
mkdir -p "$ROOT/build/bin"
for t in marmot-backend marmot-opt marmot-pcln; do
  cp "$SRC/$t" "$ROOT/build/bin/$t.new" && mv "$ROOT/build/bin/$t.new" "$ROOT/build/bin/$t"
done
echo "installed backend $(git -C "$ROOT" rev-parse --short HEAD)"
