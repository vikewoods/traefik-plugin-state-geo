#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_id="$$"
network_name="state-geo-smoke-${run_id}"
backend_name="state-geo-whoami-${run_id}"
traefik_name="state-geo-traefik-${run_id}"
traefik_image="${TRAEFIK_IMAGE:-traefik:v3.7.6}"
whoami_image="${WHOAMI_IMAGE:-traefik/whoami:v1.11.0}"
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/state-geo-smoke-${run_id}.XXXXXX")"
dynamic_config="${repo_root}/testdata/traefik/dynamic.yml"
real_artifact=false

allowed_ip="216.160.83.56"
blocked_ip="214.78.120.1"
non_us_ipv4="89.160.20.128"
non_us_ipv6="2001:218::1"

cleanup() {
  docker rm --force "${traefik_name}" "${backend_name}" >/dev/null 2>&1 || true
  docker network rm "${network_name}" >/dev/null 2>&1 || true
  rm -rf "${temporary_root}"
}
trap cleanup EXIT

if [[ "${STATE_GEO_REAL_MMDB+x}" == x ]]; then
  if [[ -z "${STATE_GEO_REAL_MMDB}" ]]; then
    printf '%s\n' 'smoke test failed: STATE_GEO_REAL_MMDB is set but empty' >&2
    exit 1
  fi

  real_artifact=true
  cases_file="${temporary_root}/real-artifact-cases"
  if ! STATE_GEO_REAL_SMOKE_CASES_FILE="${cases_file}" \
    go -C "${repo_root}" test -run '^TestRealComplianceArtifactSmokeCases$' -count=1 . \
    >"${temporary_root}/case-discovery.log" 2>&1; then
    printf '%s\n' 'smoke test failed: real artifact case discovery failed' >&2
    exit 1
  fi

  allowed_ip=""
  blocked_ip=""
  blocked_state=""
  us_ipv6=""
  us_ipv6_state=""
  non_us_ipv4=""
  non_us_ipv6=""
  artifact_size=""
  artifact_mode=""
  artifact_mtime=""
  artifact_sha256=""
  while IFS='=' read -r key value; do
    case "${key}" in
      allowed_us) allowed_ip="${value}" ;;
      blocked_us) blocked_ip="${value}" ;;
      blocked_state) blocked_state="${value}" ;;
      us_ipv6) us_ipv6="${value}" ;;
      us_ipv6_state) us_ipv6_state="${value}" ;;
      non_us_ipv4) non_us_ipv4="${value}" ;;
      non_us_ipv6) non_us_ipv6="${value}" ;;
      artifact_size) artifact_size="${value}" ;;
      artifact_mode) artifact_mode="${value}" ;;
      artifact_mtime) artifact_mtime="${value}" ;;
      artifact_sha256) artifact_sha256="${value}" ;;
      *)
        printf '%s\n' 'smoke test failed: unexpected real artifact case key' >&2
        exit 1
        ;;
    esac
  done <"${cases_file}"
  if [[ -z "${allowed_ip}" || -z "${blocked_ip}" ||
    ! "${blocked_state}" =~ ^[A-Z]{2}$ ||
    -z "${us_ipv6}" || ! "${us_ipv6_state}" =~ ^[A-Z]{2}$ ||
    -z "${non_us_ipv4}" || -z "${non_us_ipv6}" ||
    -z "${artifact_size}" || -z "${artifact_mode}" ||
    -z "${artifact_mtime}" || ! "${artifact_sha256}" =~ ^[0-9a-f]{64}$ ]]; then
    printf '%s\n' 'smoke test failed: incomplete real artifact case matrix' >&2
    exit 1
  fi

  dynamic_config="${temporary_root}/dynamic.yml"
  cat >"${dynamic_config}" <<EOF
http:
  routers:
    smoke:
      entryPoints:
        - web
      rule: PathPrefix(\`/\`)
      middlewares:
        - state-geo
      service: backend
  middlewares:
    state-geo:
      plugin:
        stateGeoBlock:
          dbPath: /data/stategeodb.mmdb
          databaseReloadInterval: 1m
          cacheSize: 0
          blockNonUS: true
          blockUSStates: true
          blockedStates:
            - ${blocked_state}
            - ${us_ipv6_state}
          clientIPHeaders:
            - CF-Connecting-IP
          trustedProxyCIDRs:
            - 0.0.0.0/0
            - ::/0
          rejectInvalidClientIPHeaders: true
          databaseFailurePolicy: deny
          lookupFailurePolicy: deny
          invalidClientIPPolicy: deny
          unknownCountryPolicy: deny
          unknownSubdivisionPolicy: deny
          privateIPPolicy: deny
          logLevel: "off"
  services:
    backend:
      loadBalancer:
        servers:
          - url: http://state-geo-whoami:80
EOF
fi

fail() {
  printf 'smoke test failed: %s\n' "$1" >&2
  if [[ "${real_artifact}" != true ]]; then
    docker logs "${traefik_name}" >&2 || true
  fi
  exit 1
}

assert_status() {
  local expected="$1"
  local name="$2"
  shift 2

  local output_file="${temporary_root}/${name}.body"
  local actual
  actual="$(curl --silent --show-error --output "${output_file}" --write-out '%{http_code}' "$@" "http://${host_address}/")"
  if [[ "${actual}" != "${expected}" ]]; then
    if [[ "${real_artifact}" != true ]]; then
      printf '%s response body:\n' "${name}" >&2
      sed -n '1,80p' "${output_file}" >&2
    fi
    fail "${name}: HTTP ${actual}, expected ${expected}"
  fi

  printf '%-32s HTTP %s\n' "${name}" "${actual}"
}

start_traefik() {
  if ! docker run --detach --rm \
    --name "${traefik_name}" \
    --network "${network_name}" \
    --publish 127.0.0.1::8000 \
    --volume "${repo_root}:/plugins-local/src/github.com/vikewoods/traefik-plugin-state-geo:ro" \
    --volume "${dynamic_config}:/etc/traefik/dynamic.yml:ro" \
    "$@" \
    "${traefik_image}" \
    --entrypoints.web.address=:8000 \
    --entrypoints.web.forwardedheaders.insecure=true \
    --providers.file.filename=/etc/traefik/dynamic.yml \
    --experimental.localplugins.stateGeoBlock.modulename=github.com/vikewoods/traefik-plugin-state-geo \
    --log.level=DEBUG >/dev/null 2>"${temporary_root}/docker-start.stderr"; then
    if [[ "${real_artifact}" != true ]]; then
      sed -n '1,80p' "${temporary_root}/docker-start.stderr" >&2
    fi
    return 1
  fi
}

docker network create "${network_name}" >/dev/null
docker run --detach --rm \
  --name "${backend_name}" \
  --network "${network_name}" \
  --network-alias state-geo-whoami \
  "${whoami_image}" >/dev/null

if [[ "${real_artifact}" == true ]]; then
  if ! start_traefik --mount "type=bind,source=${STATE_GEO_REAL_MMDB},target=/data/stategeodb.mmdb,readonly"; then
    printf '%s\n' 'smoke test failed: Traefik could not mount the real artifact' >&2
    exit 1
  fi
else
  start_traefik
fi

host_address="$(docker port "${traefik_name}" 8000/tcp | sed -n '1s/.*://p')"
host_address="127.0.0.1:${host_address}"

ready=false
for _ in {1..30}; do
  if curl --silent --output /dev/null \
    --header "CF-Connecting-IP: ${allowed_ip}" \
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

if [[ "${real_artifact}" == true ]]; then
  assert_status 200 real_us_allowed \
    --header "CF-Connecting-IP: ${allowed_ip}"
  assert_status 403 real_us_state_blocked \
    --header "CF-Connecting-IP: ${blocked_ip}"
  assert_status 403 real_us_ipv6_state_blocked \
    --header "CF-Connecting-IP: ${us_ipv6}"
  assert_status 403 real_non_us_ipv4_blocked \
    --header "CF-Connecting-IP: ${non_us_ipv4}"
  assert_status 403 real_non_us_ipv6_blocked \
    --header "CF-Connecting-IP: ${non_us_ipv6}"

  if ! docker rm --force "${traefik_name}" \
    >/dev/null 2>"${temporary_root}/docker-remove.stderr"; then
    printf '%s\n' 'smoke test failed: Traefik container cleanup failed' >&2
    exit 1
  fi

  cases_after_file="${temporary_root}/real-artifact-cases-after"
  if ! STATE_GEO_REAL_SMOKE_CASES_FILE="${cases_after_file}" \
    go -C "${repo_root}" test -run '^TestRealComplianceArtifactSmokeCases$' -count=1 . \
    >"${temporary_root}/case-discovery-after.log" 2>&1; then
    fail "real artifact post-smoke verification failed"
  fi
  if ! cmp -s "${cases_file}" "${cases_after_file}"; then
    fail "real artifact changed during interpreted smoke testing"
  fi
else
  assert_status 200 cf_ipv4_allowed \
    --header "CF-Connecting-IP: ${allowed_ip}"
  assert_status 403 cf_ipv4_blocked \
    --header "CF-Connecting-IP: ${blocked_ip}"
  assert_status 403 cf_ipv6_blocked \
    --header "CF-Connecting-IP: ${non_us_ipv6}"
  assert_status 200 true_client_ip_allowed \
    --header "True-Client-IP: ${allowed_ip}"
  assert_status 200 x_forwarded_for_allowed \
    --header "X-Forwarded-For: ${allowed_ip}, 10.17.1.20"
  assert_status 403 malformed_cf_is_rejected \
    --header 'CF-Connecting-IP: not-an-ip' \
    --header "X-Forwarded-For: ${allowed_ip}, 10.17.1.20"
  assert_status 403 missing_client_header_denied
fi

printf 'Traefik interpreted-plugin smoke test passed with %s.\n' "${traefik_image}"
