#!/bin/bash
# Starts the amd64 image that `goreleaser release --snapshot` built, and checks
# that it comes up healthy as its non-root user, with the data volume writable.
set -euo pipefail

image=$(docker image ls --format '{{.Repository}}:{{.Tag}}' ghcr.io/manasseh-fikadu/roost | grep -- '-amd64$' | head -n 1)
[ -n "$image" ] || { echo "no amd64 image of Roost was built" >&2; exit 1; }
echo "Checking $image"

docker volume create roost-check >/dev/null
trap 'docker rm -f roost-check >/dev/null 2>&1 || true; docker volume rm roost-check >/dev/null 2>&1 || true' EXIT
docker run -d --name roost-check -v roost-check:/data "$image" >/dev/null

for _ in $(seq 1 30); do
  status=$(docker inspect --format '{{.State.Health.Status}}' roost-check)
  if [ "$status" = healthy ]; then
    echo "The image is healthy."
    # A backup needs the database to exist and the volume to be writable.
    docker exec roost-check /usr/local/bin/roost backup /data/backups/check.tar.gz
    exit 0
  fi
  sleep 2
done
echo "The image didn't become healthy (it is $status):" >&2
docker logs roost-check >&2
exit 1
