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

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
known="$work/known"
cited="$work/cited"

# The top-level test names Go actually knows about.
#
# `go test -list` has to compile every package to answer, so a package that
# does not build produces no names — and this check would then report every
# citation as missing, or, under `set -e`, exit 1 having printed nothing at
# all. That is exactly what it did on CI: internal/app imports Wails, which is
# cgo over GTK on Linux, and the job had no headers installed. A check whose
# failure is indistinguishable from a crash is worse than no check, so the
# build error is captured and shown rather than sent to /dev/null.
list_tests() {
	local errors="$work/err"
	if ! go test "$@" ./internal/... -list '.*' 2>"$errors" | grep -E '^Test' | sort -u; then
		echo "the test list could not be built (go test $*):" >&2
		sed 's/^/  /' "$errors" >&2
		exit 1
	fi
}

list_tests > "$known"
list_tests -tags=integration >> "$known"
sort -u -o "$known" "$known"

# Citations are in backticks and may name a subtest after a slash; the top-level
# name is what `-list` reports, so that is what gets compared.
grep -o '`Test[A-Za-z0-9_]*' "$doc" \
  | tr -d '`' \
  | sed 's#/.*##' \
  | grep -vE '^Test$' \
  | sort -u > "$cited"

# comm needs both sides ordered the same way, and `sort` orders by locale. A
# runner whose locale differs from a developer's would otherwise disagree about
# where an underscore sorts and invent missing tests.
missing=$(LC_ALL=C comm -23 <(LC_ALL=C sort -u "$cited") <(LC_ALL=C sort -u "$known"))
if [ -n "$missing" ]; then
  echo "$doc cites tests that do not exist:"
  echo "$missing" | sed 's/^/  /'
  echo
  echo "Either the test was renamed and the matrix needs updating, or the evidence was never there."
  exit 1
fi

echo "traceability matrix: $(wc -l < "$cited" | tr -d " ") cited tests, all present"
