#!/bin/sh
# Builds the backend and runs backend/test. Arguments: test files or dirs, -v.
set -e
cd "$(dirname "$0")/.."
timeout -s KILL 1200 ./backend/build.sh >/dev/null
exec python3 backend/test/run.py "$@"
