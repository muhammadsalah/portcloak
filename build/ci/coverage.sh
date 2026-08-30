#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0

# Runs the unit suite, measures what it covered, and refuses to let that number
# go backwards.
#
# The measurement is deliberately not the default one. `go test -cover` grades
# each package against its own tests, so a package with no test file reports
# 0.0% and a package whose only exercise comes from a sibling's test reports the
# same — while the module-wide figure Go prints is an average over packages
# rather than over code. `-coverpkg=./internal/...` instead instruments every
# package for every test binary, so a store fake exercised through the
# orchestrator counts where it is written. The number that produces is lower
# than the per-package average and is the honest one.
#
# The floor is a ratchet, not a target. It is set just under what the suite
# currently reaches, so the only way to change it is upwards, deliberately, in
# a commit that says why.
set -euo pipefail

# internal/desktop imports Wails, which is cgo over GTK on Linux, and `gtk3` is
# what selects the webkit2gtk-4.1 backend the runners actually install. Same
# reasoning, and the same requirement to stay in step, as the matrix in
# .github/workflows/ci.yml — see check-traceability.sh, which sets it the same
# way and for the same reason.
#
# One package needs this, not the tree: internal/desktop is the window and the
# menu, and everything the CLI reaches compiles with CGO_ENABLED=0. The scope is
# still ./internal/... rather than a hand-written list of the packages that do
# not need it, because such a list rots silently — a new top-level package would
# drop out of the coverage denominator with nothing to say so.
tags="gtk3"
if [ "$(uname -s)" != "Linux" ]; then
	tags=""
fi

profile="${COVERAGE_PROFILE:-coverage.out}"
# Raise it by editing this line, never by lowering it to make a branch green.
# The suite reaches 57.6% as of the commit that added this script; the couple of
# points of slack absorb the difference between a developer's machine and a
# runner, not a test that was deleted.
floor="${COVERAGE_FLOOR:-55}"

go test -tags "$tags" ./internal/... \
	-race \
	-timeout 15m \
	-covermode=atomic \
	-coverpkg=./internal/... \
	-coverprofile="$profile"

total=$(go tool cover -func="$profile" | awk '$1 == "total:" { print $3 }' | tr -d '%')

# Per package, worst first: the top of that list is where the next test belongs,
# and it is the only part of this output anyone reads twice.
packages=$(go tool cover -func="$profile" \
	| awk '$1 != "total:" {
		file = $1
		sub(/:[0-9]+:$/, "", file)
		sub(/\/[^\/]*$/, "", file)
		statements[file]++
		if ($3 != "0.0%") covered[file]++
	}
	END {
		for (pkg in statements) {
			printf "%6.1f%%  %s\n", 100 * covered[pkg] / statements[pkg], pkg
		}
	}' | sort -n)

echo
echo "Coverage by package, least covered first:"
echo "$packages" | sed 's/^/  /'
echo
echo "Total: ${total}% of statements (floor ${floor}%)"

# GitHub renders this under the job, so the number is visible without opening
# the log — which is the difference between a metric and a line of output.
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	{
		echo "### Go coverage: ${total}%"
		echo
		echo "Floor is ${floor}%, measured with \`-coverpkg=./internal/...\` over the whole module."
		echo
		echo "<details><summary>By package, least covered first</summary>"
		echo
		echo '```'
		echo "$packages"
		echo '```'
		echo
		echo "</details>"
	} >> "$GITHUB_STEP_SUMMARY"
fi

# Shell has no floats, and neither does `[`. awk does.
if awk -v total="$total" -v floor="$floor" 'BEGIN { exit !(total < floor) }'; then
	echo
	echo "Coverage fell to ${total}%, below the ${floor}% floor."
	echo "Either the change needs a test, or the floor needs lowering in build/ci/coverage.sh"
	echo "with a commit message saying which tests were removed and why."
	exit 1
fi
