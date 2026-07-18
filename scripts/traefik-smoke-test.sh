#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_id="$$"
network_name="state-geo-smoke-${run_id}"
backend_name="state-geo-whoami-${run_id}"
traefik_name="state-geo-traefik-${run_id}"
traefik_image="${TRAEFIK_IMAGE:-traefik:v3.7.1}"
whoami_image="${WHOAMI_IMAGE:-traefik/whoami:v1.11.0}"

cleanup() {
  docker rm --force "${traefik_name}" "${backend_name}" >/dev/null 2>&1 || true
  docker network rm "${network_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  printf 'smoke test failed: %s\n' "$1" >&2
  docker logs "${traefik_name}" >&2 || true
  exit 1
}

assert_status() {
  local expected="$1"
  local name="$2"
  shift 2

  local output_file="${TMPDIR:-/tmp}/state-geo-${run_id}-${name}.body"
  local actual
  actual="$(curl --silent --show-error --output "${output_file}" --write-out '%{http_code}' "$@" "http://${host_address}/")"
  if [[ "${actual}" != "${expected}" ]]; then
    printf '%s response body:\n' "${name}" >&2
    sed -n '1,80p' "${output_file}" >&2
    fail "${name}: HTTP ${actual}, expected ${expected}"
  fi

  printf '%-32s HTTP %s\n' "${name}" "${actual}"
}

docker network create "${network_name}" >/dev/null
docker run --detach --rm \
  --name "${backend_name}" \
  --network "${network_name}" \
  --network-alias state-geo-whoami \
  "${whoami_image}" >/dev/null

docker run --detach --rm \
  --name "${traefik_name}" \
  --network "${network_name}" \
  --publish 127.0.0.1::8000 \
  --volume "${repo_root}:/plugins-local/src/github.com/vikewoods/traefik-plugin-state-geo:ro" \
  --volume "${repo_root}/testdata/traefik/dynamic.yml:/etc/traefik/dynamic.yml:ro" \
  "${traefik_image}" \
  --entrypoints.web.address=:8000 \
  --entrypoints.web.forwardedheaders.insecure=true \
  --providers.file.filename=/etc/traefik/dynamic.yml \
  --experimental.localplugins.stateGeoBlock.modulename=github.com/vikewoods/traefik-plugin-state-geo \
  --log.level=DEBUG >/dev/null

host_address="$(docker port "${traefik_name}" 8000/tcp | sed -n '1s/.*://p')"
host_address="127.0.0.1:${host_address}"

ready=false
for _ in {1..30}; do
  if curl --silent --output /dev/null \
    --header 'CF-Connecting-IP: 216.160.83.56' \
    "http://${host_address}/"; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "${ready}" != true ]]; then
  fail "Traefik did not become ready"
fi

docker logs "${traefik_name}" 2>&1 | grep --fixed-strings 'Plugins loaded.' >/dev/null || fail "plugin was not loaded"

assert_status 200 cf_ipv4_allowed \
  --header 'CF-Connecting-IP: 216.160.83.56'
assert_status 403 cf_ipv4_blocked \
  --header 'CF-Connecting-IP: 214.78.120.1'
assert_status 403 cf_ipv6_blocked \
  --header 'CF-Connecting-IP: 2001:218::1'
assert_status 200 true_client_ip_allowed \
  --header 'True-Client-IP: 216.160.83.56'
assert_status 200 x_forwarded_for_allowed \
  --header 'X-Forwarded-For: 216.160.83.56, 10.17.1.20'
assert_status 200 malformed_cf_falls_back_to_xff \
  --header 'CF-Connecting-IP: not-an-ip' \
  --header 'X-Forwarded-For: 216.160.83.56, 10.17.1.20'
assert_status 403 missing_client_header_denied

printf 'Traefik interpreted-plugin smoke test passed with %s.\n' "${traefik_image}"
