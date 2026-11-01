#!/bin/bash
# Starts the amd64 image that `goreleaser release --snapshot` built, and checks
# that it comes up healthy as its non-root user, with the data volume writable.
set -euo pipefail

image=$(docker image ls --format '{{.Repository}}:{{.Tag}}' ghcr.io/manasseh-fikadu/bield | grep -- '-amd64$' | head -n 1)
[ -n "$image" ] || { echo "no amd64 image of Bield was built" >&2; exit 1; }
echo "Checking $image"

docker volume create bield-check >/dev/null
trap 'docker rm -f bield-check >/dev/null 2>&1 || true; docker volume rm bield-check >/dev/null 2>&1 || true' EXIT
docker run -d --name bield-check -v bield-check:/data "$image" >/dev/null

for _ in $(seq 1 30); do
  status=$(docker inspect --format '{{.State.Health.Status}}' bield-check)
  if [ "$status" = healthy ]; then
    echo "The image is healthy."
    # A backup needs the database to exist and the volume to be writable.
    docker exec bield-check /usr/local/bin/bield backup /data/backups/check.tar.gz
    exit 0
  fi
  sleep 2
done
echo "The image didn't become healthy (it is $status):" >&2
docker logs bield-check >&2
exit 1
