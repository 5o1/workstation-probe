#!/usr/bin/env bash
# Smoke test for the workstation-probe server. Assumes the binary is already
# running at the URL given by $1 (default http://localhost:19090).
#
# Exits non-zero on the first failure; each assertion prints PASS/FAIL.

set -u

URL="${1:-http://localhost:19090}"
fail=0

# Wait up to 10s for the server to be ready before running assertions.
for i in $(seq 1 50); do
  if curl -sf -o /dev/null "$URL/health"; then
    break
  fi
  sleep 0.2
  if [ "$i" = "50" ]; then
    echo "FAIL  server not ready at $URL (timeout)"
    exit 1
  fi
done

check() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    echo "PASS  $label  ($actual)"
  else
    echo "FAIL  $label  expected=$expected actual=$actual"
    fail=1
  fi
}

status() {
  curl -s -o /dev/null -w "%{http_code}" "$1"
}

has_field() {
  local body="$1" field="$2"
  echo "$body" | grep -q "\"$field\""
}

echo "Smoke testing $URL"

# 1) /health
code=$(status "$URL/health")
check "GET /health 200" "200" "$code"

# 2) /profile
code=$(status "$URL/profile")
check "GET /profile 200" "200" "$code"
body=$(curl -sf "$URL/profile")
if has_field "$body" "hostname"; then
  echo "PASS  /profile contains hostname"
else
  echo "FAIL  /profile missing hostname"
  fail=1
fi
if has_field "$body" "modules"; then
  echo "PASS  /profile contains modules"
else
  echo "FAIL  /profile missing modules"
  fail=1
fi

# 3) /metrics (combined)
code=$(status "$URL/metrics")
check "GET /metrics 200" "200" "$code"

# 4) per-module endpoints (expect at least one 200, others depend on host)
for mod in cpu memory storage gpu; do
  code=$(status "$URL/metrics/$mod")
  case "$mod" in
    gpu)
      # gpu may be 404 if no NVML / disabled
      if [[ "$code" == "200" || "$code" == "404" ]]; then
        echo "PASS  GET /metrics/$mod (got $code — ok)"
      else
        echo "FAIL  GET /metrics/$mod unexpected status $code"
        fail=1
      fi
      ;;
    *)
      check "GET /metrics/$mod 200" "200" "$code"
      ;;
  esac
done

# 5) /metrics/cpu/history
code=$(status "$URL/metrics/cpu/history?duration=5s")
check "GET /metrics/cpu/history?duration=5s 200" "200" "$code"

# 6) bad duration → 400
code=$(status "$URL/metrics/cpu/history?duration=abc")
check "GET /metrics/cpu/history?duration=abc 400" "400" "$code"

# 7) unknown route → 404 (Go 1.22 ServeMux returns 404 for unregistered paths)
code=$(status "$URL/does-not-exist")
check "GET /does-not-exist 404" "404" "$code"

if [[ $fail -ne 0 ]]; then
  echo "SMOKE TEST: FAILED"
  exit 1
fi
echo "SMOKE TEST: PASSED"
exit 0
