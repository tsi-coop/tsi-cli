#!/usr/bin/env bash
# Prints catalog migration steps with checksums, from a local checkout of a module at a ref.
#   hack/sums.sh ~/tsi-privacy-vault v0.3.0 db
# Paste the output under migrations.steps in catalog/modules/<module>.yaml, set
# migrations.source.ref to the same ref, and run `make network-test` to confirm.
set -euo pipefail
repo="${1:?path to the module checkout}"; ref="${2:?tag or commit}"; dir="${3:-db}"
git -C "$repo" ls-tree --name-only "$ref" "$dir/" | grep -E '\.sql$' | sort | while read -r path; do
  printf '    - {file: %s, sha256: %s}\n' "$(basename "$path")" "$(git -C "$repo" show "$ref:$path" | sha256sum | cut -d' ' -f1)"
done
