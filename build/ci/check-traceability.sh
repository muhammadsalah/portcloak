#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0

# Every test named in the rollout traceability matrix must exist.
#
# The matrix is only worth reading if its evidence column is real. Without this
# check a renamed test turns a citation into fiction silently, and the document
# degrades into a list of things that were once true.
set -euo pipefail

doc="spec/rollout/12-rollout-traceability.md"

# The top-level test names Go actually knows about.
go test ./internal/... -list '.*' 2>/dev/null | grep -E '^Test' | sort -u > /tmp/portcloak-known-tests
go test -tags=integration ./internal/... -list '.*' 2>/dev/null | grep -E '^Test' | sort -u >> /tmp/portcloak-known-tests
sort -u -o /tmp/portcloak-known-tests /tmp/portcloak-known-tests

# Citations are in backticks and may name a subtest after a slash; the top-level
# name is what `-list` reports, so that is what gets compared.
grep -o '`Test[A-Za-z0-9_]*' "$doc" \
  | tr -d '`' \
  | sed 's#/.*##' \
  | grep -vE '^Test$' \
  | sort -u > /tmp/portcloak-cited-tests

missing=$(comm -23 /tmp/portcloak-cited-tests /tmp/portcloak-known-tests)
if [ -n "$missing" ]; then
  echo "$doc cites tests that do not exist:"
  echo "$missing" | sed 's/^/  /'
  echo
  echo "Either the test was renamed and the matrix needs updating, or the evidence was never there."
  exit 1
fi

echo "traceability matrix: $(wc -l < /tmp/portcloak-cited-tests) cited tests, all present"
