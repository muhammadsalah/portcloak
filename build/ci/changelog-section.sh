#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0
#
# Prints one version's section of CHANGELOG.md, for the release notes.
#
# The release workflow drafts notes describing how a build was verified and what
# it was signed with. What it could not say was what changed, so that half was
# written by hand into the draft or not at all — and "or not at all" is what
# actually happens on a release cut in a hurry.
#
# The changelog is already the record, kept in the same commit as the change it
# describes. This lifts the section out of it rather than asking anyone to
# rewrite it, which is also what stops the two disagreeing.
#
# Usage:
#   build/ci/changelog-section.sh 0.0.2 [CHANGELOG.md]
#
# Exits non-zero, loudly, when the version has no section. A release drafted
# without one would be published saying nothing about what changed, and the
# person who would notice is the one reading it a month later.

set -euo pipefail

version=${1:-}
file=${2:-CHANGELOG.md}

if [ -z "$version" ]; then
	echo "usage: $0 <version> [changelog]" >&2
	exit 2
fi
if [ ! -f "$file" ]; then
	echo "!! no changelog at $file" >&2
	exit 2
fi

# The heading is matched literally rather than as a pattern: a version is full
# of dots, and `0.0.2` as a regex also matches `0x0y2`. Awk's index() takes the
# needle as a plain string, which is what this needs.
#
# Everything between that heading and the next `## ` heading is the section. The
# body is printed without its own heading — the release is already titled with
# the version, and repeating it reads as a mistake.
section=$(
	awk -v needle="## [$version]" '
		index($0, needle) == 1 { found = 1; next }
		found && /^## / { exit }
		found { print }
	' "$file"
)

# Strip the blank lines the split leaves at both ends, so the caller can place
# this between other blocks without guessing.
section=$(printf '%s\n' "$section" | sed -e '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;};/\n$/ba')

if [ -z "$section" ]; then
	echo "!! CHANGELOG.md has no section for $version." >&2
	echo "   Add a '## [$version] — <date>' heading with what changed, then release again." >&2
	echo "   Releasing without it publishes notes that say how the build was verified" >&2
	echo "   and nothing about what is in it." >&2
	exit 1
fi

printf '%s\n' "$section"
