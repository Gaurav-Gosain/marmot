#!/bin/sh
# Installs the backend tools from the private build into build/bin: only from a committed backend tree, after the backend tests passed on
# that build. Run backend/build.sh and backend/test.sh first.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC=${BACKEND_BIN_DIR:-$ROOT/build/backend-test/bin}
if ! git -C "$ROOT" diff --quiet HEAD -- backend; then
  echo "install.sh: backend/ has uncommitted changes; commit first" >&2
  exit 1
fi
if [ ! -f "$SRC/.tested" ] || [ "$(cat "$SRC/.tested")" != "$(git -C "$ROOT" rev-parse HEAD:backend)" ]; then
  echo "install.sh: no passing test run for this backend tree; run backend/test.sh" >&2
  exit 1
fi
mkdir -p "$ROOT/build/bin"
for t in marmot-backend marmot-opt marmot-pcln; do
  cp "$SRC/$t" "$ROOT/build/bin/$t.new" && mv "$ROOT/build/bin/$t.new" "$ROOT/build/bin/$t"
done
echo "installed backend $(git -C "$ROOT" rev-parse --short HEAD)"
