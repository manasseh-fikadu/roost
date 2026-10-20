#!/usr/bin/env bash
# Checks that every commit in BASE..HEAD has a Signed-off-by line with its
# author's email (see "Sign your work" in CONTRIBUTING.md). Bots are skipped,
# since only people can certify the DCO.
#
# Usage: check-signoff.sh BASE HEAD
set -euo pipefail

base=$1
head=$2
unsigned=0

for commit in $(git rev-list --no-merges "$base..$head"); do
  email=$(git show -s --format='%ae' "$commit")
  case $email in
    *'[bot]@users.noreply.github.com') continue ;;
  esac
  if ! git show -s --format='%(trailers:key=Signed-off-by,valueonly)' "$commit" | grep -qiF "<$email>"; then
    git show -s --format='%h %s' "$commit"
    unsigned=$((unsigned + 1))
  fi
done

if [ "$unsigned" -gt 0 ]; then
  echo "::error::$unsigned commit(s) above have no Signed-off-by line with the author's email. \"Sign your work\" in CONTRIBUTING.md shows how to fix that."
  exit 1
fi
echo "Every commit is signed off."
