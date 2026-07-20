#!/usr/bin/env bash

set -euo pipefail

release_version="${RELEASE_VERSION:-v2.0.0-rc.2}"
module_name="$(awk 'NR == 1 && $1 == "module" { print $2 }' go.mod)"

if [[ "${release_version}" =~ ^v([0-9]+)\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  release_major="v${BASH_REMATCH[1]}"
else
  printf 'invalid release version for manifest check: %s\n' "${release_version}" >&2
  exit 1
fi

if [[ "${release_major}" =~ ^v([2-9]|[1-9][0-9]+)$ ]] &&
  [[ "${module_name}" != */"${release_major}" ]]; then
  printf 'module %s must end in /%s for release %s\n' \
    "${module_name}" "${release_major}" "${release_version}" >&2
  exit 1
fi

ruby -ryaml -rjson -e '
  manifest = YAML.safe_load(File.read(".traefik.yml"), aliases: false)
  required = %w[displayName type import basePkg summary testData]
  missing = required.reject { |key| manifest.key?(key) }
  abort("missing manifest fields: #{missing.join(", ")}") unless missing.empty?
  abort("manifest type must be middleware") unless manifest["type"] == "middleware"
  module_name = File.foreach("go.mod").first.split.fetch(1)
  abort("manifest import does not match go.mod") unless manifest["import"] == module_name
  package_names = Dir.glob("*.go").filter_map do |path|
    File.foreach(path).find { |line| line.match?(/^package\s+/) }&.split&.fetch(1, nil)
  end.uniq
  unless package_names == [manifest["basePkg"]]
    abort("manifest basePkg does not match root Go package")
  end
  puts JSON.generate(manifest.fetch("testData"))
' | go run ./internal/manifestcheck
