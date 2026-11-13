#!/bin/sh
# build.sh [IMAGE]: build the compile service image from the committed tree (HEAD), for
# linux/amd64, with docker or podman. JOBS (default 6) caps the backend's compile jobs; with
# podman, CPUS and MEM cap the build (default 0-5 and 10g).
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
IMAGE=${1:-marmot-play:latest}
ctx=$(mktemp -d)
trap 'rm -rf "$ctx"' EXIT
git -C "$ROOT" archive HEAD | tar -x -C "$ctx"
if command -v podman >/dev/null 2>&1; then
	podman build --platform linux/amd64 --cpuset-cpus "${CPUS:-0-5}" --memory "${MEM:-10g}" \
		--build-arg JOBS="${JOBS:-6}" -f "$ctx/deploy/play/Dockerfile" -t "$IMAGE" "$ctx"
else
	docker build --platform linux/amd64 --build-arg JOBS="${JOBS:-6}" -f "$ctx/deploy/play/Dockerfile" -t "$IMAGE" "$ctx"
fi
