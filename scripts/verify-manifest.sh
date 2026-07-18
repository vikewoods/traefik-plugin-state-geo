#!/usr/bin/env bash

set -euo pipefail

ruby -ryaml -rjson -e '
  manifest = YAML.safe_load(File.read(".traefik.yml"), aliases: false)
  required = %w[displayName type import summary testData]
  missing = required.reject { |key| manifest.key?(key) }
  abort("missing manifest fields: #{missing.join(", ")}") unless missing.empty?
  abort("manifest type must be middleware") unless manifest["type"] == "middleware"
  module_name = File.foreach("go.mod").first.split.fetch(1)
  abort("manifest import does not match go.mod") unless manifest["import"] == module_name
  puts JSON.generate(manifest.fetch("testData"))
' | go run ./internal/manifestcheck
