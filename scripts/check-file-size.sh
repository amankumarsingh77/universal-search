#!/usr/bin/env bash
set -euo pipefail

# REF-021: files in internal/app/ must stay under 400 lines (production code only).
VIOLATIONS=$(find internal/app -name '*.go' -not -name '*_test.go' -exec wc -l {} \; | awk '$1 > 400 { print $2 " (" $1 " lines)" }')
if [ -n "$VIOLATIONS" ]; then
  echo "Files in internal/app/ exceeding 400 lines:"
  echo "$VIOLATIONS"
  exit 1
fi
