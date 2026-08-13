#!/usr/bin/env bash
set -euo pipefail

admin_url="${ADMIN_URL:-http://127.0.0.1:9090}"
proxy_url="${PROXY_URL:-http://127.0.0.1:8080}"
timeout_seconds="${SMOKE_TIMEOUT_SECONDS:-180}"
deadline=$((SECONDS + timeout_seconds))

printf 'Waiting for EgressShuffle readiness'
until curl --fail --silent --show-error "${admin_url}/readyz" >/dev/null; do
  if (( SECONDS >= deadline )); then
    printf '\nEgressShuffle did not become ready within %s seconds\n' "${timeout_seconds}" >&2
    exit 1
  fi
  printf '.'
  sleep 2
done
printf '\n'

curl --fail --silent --show-error "${admin_url}/healthz" >/dev/null
curl --fail --silent --show-error "${admin_url}/metrics" | grep -q '^egressshuffle_backend_count '
curl --fail --silent --show-error --proxy "${proxy_url}" http://example.com/ >/dev/null
curl --fail --silent --show-error --proxy "${proxy_url}" https://example.com/ >/dev/null

printf 'Smoke test passed\n'
