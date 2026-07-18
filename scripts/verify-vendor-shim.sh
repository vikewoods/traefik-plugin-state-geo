#!/usr/bin/env bash

set -euo pipefail

vendor_dir="vendor/github.com/oschwald/maxminddb-golang"
shim="${vendor_dir}/shim_pure.go"

[[ -f "${shim}" ]] || {
  printf 'missing Yaegi pure-reader shim: %s\n' "${shim}" >&2
  exit 1
}

for forbidden in reader_mmap.go mmap_unix.go mmap_windows.go; do
  if [[ -e "${vendor_dir}/${forbidden}" ]]; then
    printf 'Yaegi-incompatible vendored file is present: %s\n' "${vendor_dir}/${forbidden}" >&2
    exit 1
  fi
done

grep --fixed-strings 'os.ReadFile(file)' "${shim}" >/dev/null || {
  printf 'pure-reader shim no longer uses os.ReadFile\n' >&2
  exit 1
}

printf 'vendored MaxMind Yaegi shim is present and mmap files are absent.\n'
