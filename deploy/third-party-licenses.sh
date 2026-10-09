#!/usr/bin/env bash
# Writes the license texts of everything linked into the binary (every Go module it depends on, plus the Go
# standard library) to the file given as the first argument. Used by the release workflow, so that each archive
# carries the notices the BSD and MIT licenses ask for.
#
#   deploy/third-party-licenses.sh THIRD_PARTY_LICENSES.txt
set -euo pipefail

OUT=${1:?usage: third-party-licenses.sh <output-file>}
cd "$(dirname "$0")/.."

{
  echo "DeltaBacklog is licensed under the MIT license (see LICENSE)."
  echo "The binary also contains the following third-party software."
  echo

  go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/deltabacklog | sort -u |
    while read -r path version dir; do
      files=$(find "$dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' \) | sort)
      [ -n "$files" ] || { echo "no license file found for $path" >&2; exit 1; }
      echo "================================================================================"
      echo "$path $version"
      echo "================================================================================"
      for file in $files; do cat "$file"; echo; done
      echo
    done

  echo "================================================================================"
  echo "Go standard library $(go env GOVERSION)"
  echo "================================================================================"
  cat "$(go env GOROOT)/LICENSE"
} > "$OUT"
echo "wrote $OUT ($(grep -c '^=====' "$OUT") sections)"
