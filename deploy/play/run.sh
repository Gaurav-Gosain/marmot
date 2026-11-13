#!/bin/sh
# run.sh [IMAGE]: run the compile service image on this machine for testing, the way it is
# confined: a read-only root, a tmpfs /tmp for the requests' temp dirs, and named volumes for
# the two caches the service writes (marmot's build/cache and the go command's cache). A new
# named volume starts as a copy of the image's directory, so the warm standard library cache
# built into the image is kept. Listens on 127.0.0.1:PORT (default 7393).
#
#   PORT, NAME (default marmot-play), CPUS (default 2), MEM (default 4g)
#   run.sh -rm [NAME]   stop the container and remove its volumes
#
# Uses podman when present, otherwise docker.
set -eu
engine=docker
command -v podman >/dev/null 2>&1 && engine=podman
if [ "${1:-}" = "-rm" ]; then
	NAME=${2:-${NAME:-marmot-play}}
	$engine rm -f "$NAME" >/dev/null 2>&1 || true
	$engine volume rm "$NAME-cache" "$NAME-gocache" >/dev/null 2>&1 || true
	exit 0
fi
IMAGE=${1:-marmot-play:dev}
NAME=${NAME:-marmot-play}
$engine run -d --rm --name "$NAME" --read-only \
	--cpus "${CPUS:-2}" --memory "${MEM:-4g}" \
	--tmpfs /tmp:size=1g,mode=1777 \
	-v "$NAME-cache:/marmot/build/cache" \
	-v "$NAME-gocache:/home/play/.cache" \
	-p "127.0.0.1:${PORT:-7393}:8080" "$IMAGE" >/dev/null
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -fs "http://127.0.0.1:${PORT:-7393}/healthz" >/dev/null 2>&1; then
		echo "$NAME: http://127.0.0.1:${PORT:-7393}/compile"
		exit 0
	fi
	sleep 1
done
$engine logs "$NAME" >&2
exit 1
