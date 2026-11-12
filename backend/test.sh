#!/bin/sh
# Builds the backend into build/backend-test/bin and runs backend/test against it.
# Arguments: test files or dirs, -v. A full run that passes on a committed
# backend tree is recorded, so backend/install.sh may install that build.
#
# BACKEND_HEAD=1: builds and tests the committed backend (HEAD) in a detached
# worktree at build/backend-head/tree instead, whatever the working tree has
# uncommitted; a full passing run is recorded in build/backend-head/bin. The
# worktree uses this tree's runtime (build/lib) and marmot driver.
set -e
cd "$(dirname "$0")/.."
if [ "${BACKEND_HEAD:-}" = 1 ]; then
  R=$PWD
  H=$R/build/backend-head
  if [ ! -e "$H/tree/.git" ]; then
    rm -rf "$H/tree"
    git worktree add --detach "$H/tree" HEAD >/dev/null
  else
    # Only files that changed are rewritten, so the build stays incremental.
    git -C "$H/tree" checkout -q --detach "$(git rev-parse HEAD)"
  fi
  mkdir -p "$H/tree/build/bin"
  [ -e "$H/tree/build/lib" ] || ln -s "$R/build/lib" "$H/tree/build/lib"
  [ -e "$H/tree/build/bin/marmot" ] || ln -s "$R/build/bin/marmot" "$H/tree/build/bin/marmot"
  BACKEND_BIN_DIR=$H/bin BACKEND_BUILD_DIR=$H/cmake exec "$H/tree/backend/test.sh" "$@"
fi
BIN=${BACKEND_BIN_DIR:-$PWD/build/backend-test/bin}
BACKEND_BIN_DIR=$BIN BACKEND_BUILD_DIR=${BACKEND_BUILD_DIR:-$PWD/build/backend-test/cmake} \
  timeout -s KILL 1200 ./backend/build.sh >/dev/null
rm -f "$BIN/.tested"
BACKEND_BIN_DIR=$BIN python3 backend/test/run.py "$@"
if [ $# -eq 0 ] && git diff --quiet HEAD -- backend; then
  git rev-parse HEAD:backend > "$BIN/.tested"
fi
