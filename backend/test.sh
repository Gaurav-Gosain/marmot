#!/bin/sh
# Builds the backend into .work/backend/bin and runs backend/test against it.
# Arguments: test files or dirs, -v. A full run that passes on a committed
# backend tree is recorded, so backend/install.sh may install that build.
set -e
cd "$(dirname "$0")/.."
BIN=${BACKEND_BIN_DIR:-$PWD/.work/backend/bin}
BACKEND_BIN_DIR=$BIN BACKEND_BUILD_DIR=${BACKEND_BUILD_DIR:-$PWD/.work/backend/cmake} \
  timeout -s KILL 1200 ./backend/build.sh >/dev/null
rm -f "$BIN/.tested"
BACKEND_BIN_DIR=$BIN python3 backend/test/run.py "$@"
if [ $# -eq 0 ] && git diff --quiet HEAD -- backend; then
  git rev-parse HEAD:backend > "$BIN/.tested"
fi
