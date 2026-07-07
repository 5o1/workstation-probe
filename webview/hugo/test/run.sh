#!/usr/bin/env bash
# run.sh — build every site under exampleSite/ and diff against
# test/expected/<site>/. With --update, overwrite expected/ from the
# current build output.
#
# Run from anywhere; paths are anchored to the script's location.

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
SITES="$HERE/../exampleSite"
EXPECTED="$HERE/expected"
UPDATE=0

if [ "${1:-}" = "--update" ]; then
  UPDATE=1
fi

if ! command -v hugo >/dev/null 2>&1; then
  echo "hugo not found in PATH" >&2
  exit 2
fi

pass=0
fail=0
sites_run=0

for site_dir in "$SITES"/*/; do
  [ -d "$site_dir" ] || continue
  name=$(basename "$site_dir")
  sites_run=$((sites_run + 1))
  echo "=== $name ==="

  # Clean before build to catch stale-file drift.
  rm -rf "$site_dir/public" "$site_dir/resources" "$site_dir/.hugo_build.lock"
  if ! (cd "$site_dir" && hugo >/dev/null 2>&1); then
    echo "  BUILD FAILED"
    (cd "$site_dir" && hugo) || true
    fail=$((fail+1))
    continue
  fi

  actual="$site_dir/public"
  target="$EXPECTED/$name"

  if [ $UPDATE -eq 1 ]; then
    rm -rf "$target"
    mkdir -p "$target"
    cp -r "$actual/." "$target/"
    echo "  updated $target"
    pass=$((pass+1))
    continue
  fi

  if [ ! -d "$target" ]; then
    echo "  no expected snapshot at $target — run with --update to seed"
    fail=$((fail+1))
    continue
  fi

  if diff_out=$(diff -r "$target" "$actual" 2>&1); then
    echo "  PASS"
    pass=$((pass+1))
  else
    echo "  FAIL"
    echo "$diff_out" | head -80 | sed 's/^/    /'
    fail=$((fail+1))
  fi
done

if [ $sites_run -eq 0 ]; then
  echo "no test sites found under $SITES"
  exit 2
fi

echo
if [ $UPDATE -eq 1 ]; then
  echo "=== Updated $pass snapshot(s). Review with git diff, then commit. ==="
else
  echo "=== Summary: $pass pass, $fail fail ==="
fi
[ $fail -eq 0 ]