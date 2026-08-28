#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0

#
# Builds distributable PortCloak artifacts for every platform this host can
# produce, on both x86-64 and arm64.
#
#   ./build/package.sh                          # everything possible here
#   ./build/package.sh --version 0.0.1
#   ./build/package.sh --targets windows,linux
#   ./build/package.sh --skip-frontend          # reuse an existing frontend/dist
#   ./build/package.sh --stage-only             # build and assemble, do not archive
#   ./build/package.sh --archive-only           # archive what is already staged
#   ./build/package.sh --linux-docker           # containerise Linux even on Linux
#   ./build/package.sh --linux-arch arm64       # just one Linux architecture
#
# Output lands in dist/ with a SHA256SUMS covering all of it.
#
# The last two flags exist because a signature has to go on the bundle, not on
# the zip around it. A release therefore runs this in two passes — stage, sign,
# archive — and the signing happens in between, in the workflow, where the
# credentials are. Run without them it does both passes back to back, which is
# what a developer building locally wants.
#
# | target  | arches        | how |
# |---------|---------------|-----|
# | darwin  | arm64, amd64  | native cgo, lipo'd into one universal .app. macOS host only. |
# | windows | amd64, arm64  | cross-compiled from any host: Wails' Windows backend is pure Go, so CGO_ENABLED=0 works and no C cross-toolchain is needed. |
# | linux   | amd64, arm64  | Docker. See build/linux/Dockerfile for why this one cannot be cross-compiled with the Go toolchain alone. |
#
# What separates these from `go build ./cmd/portcloak`:
#
#   * -tags production. The switch Wails reads to compile out the inspector,
#     and that PortCloak reads to drop the Developer menu and disable the
#     webview's context menu. Without it a release ships "Inspect Element".
#   * -trimpath and -ldflags "-s -w". Build paths and the symbol table go.
#   * -H windowsgui on Windows. Without it the binary is a console executable
#     and Windows opens a terminal behind the app window.
#   * version, commit and build date stamped in.
#
# Signing and notarisation are deliberately NOT here: they need credentials
# that must not sit in a script a pull request can run. See the release gate in
# spec/rollout/11-release-0.0.1.md.

set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

version=""
targets=""
skip_frontend=0
do_build=1
do_archive=1
linux_docker=0
linux_arches="amd64 arm64"
while [ $# -gt 0 ]; do
	case "$1" in
	--version) version="$2"; shift 2 ;;
	--targets) targets="$2"; shift 2 ;;
	--skip-frontend) skip_frontend=1; shift ;;
	--stage-only) do_archive=0; shift ;;
	--archive-only) do_build=0; skip_frontend=1; shift ;;
	--linux-docker) linux_docker=1; shift ;;
	--linux-arch) linux_arches="$2"; shift 2 ;;
	-h | --help) sed -n '2,42p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

have() { command -v "$1" >/dev/null 2>&1; }

host=$(uname -s)
if [ -z "$targets" ]; then
	targets="windows"
	[ "$host" = "Darwin" ] && targets="darwin,$targets"
	if [ "$host" = "Linux" ] || docker info >/dev/null 2>&1; then
		targets="$targets,linux"
	fi
fi

# A tag if we are on one, otherwise the nearest tag plus distance and hash.
if [ -z "$version" ]; then
	version=$(git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-unknown")
	version=${version#spec-}
	version=${version#v}
fi
commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
[ -n "$(git status --porcelain 2>/dev/null)" ] && commit="${commit}-dirty"
date=$(date -u -r "${SOURCE_DATE_EPOCH:-$(date +%s)}" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
	date -u +%Y-%m-%dT%H:%M:%SZ)

echo "PortCloak $version ($commit) $date"
echo "targets: $targets"
echo

dist="$root/dist"
# Everything assembled but not yet archived lives here. Keeping it in one place
# is what lets the signing pass in between find it without knowing how any
# particular platform lays its bundle out.
stage="$dist/stage"
if [ "$do_build" -eq 1 ]; then
	rm -rf "$dist"
fi
mkdir -p "$dist" "$stage"

# ------------------------------------------------------------------ frontend
#
# The Go binary embeds frontend/dist. Building without rebuilding the frontend
# is the one mistake that ships a release running last week's UI, so skipping
# takes an explicit flag.
if [ "$skip_frontend" -eq 0 ]; then
	echo "==> frontend"
	npm --prefix frontend ci
	npm --prefix frontend run build
else
	echo "==> frontend (skipped)"
fi
if [ "$do_build" -eq 1 ] && [ ! -s frontend/dist/index.html ]; then
	echo "frontend/dist/index.html is missing or empty — refusing to package a binary with no UI" >&2
	exit 1
fi

ldflags_common="-s -w -X main.version=$version -X main.commit=$commit -X main.date=$date"

wants() { case ",$targets," in *",$1,"*) return 0 ;; *) return 1 ;; esac; }

# =========================================================================
# macOS — one universal .app
# =========================================================================
build_darwin() {
	if [ "$host" != "Darwin" ]; then
		echo "!! skipping darwin: an .app needs a macOS host (lipo, codesign)" >&2
		return
	fi
	echo "==> darwin/arm64 + darwin/amd64"
	local out="$stage/macos-universal"
	local app="$out/PortCloak.app"
	rm -rf "$out"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

	local slices=()
	for arch in arm64 amd64; do
		if GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 \
			go build -tags production -trimpath -ldflags "$ldflags_common" \
			-o "$dist/.pc-$arch" ./cmd/portcloak 2>/dev/null; then
			slices+=("$dist/.pc-$arch")
		else
			echo "!! darwin/$arch slice failed to build" >&2
		fi
	done
	if [ ${#slices[@]} -eq 0 ]; then
		echo "!! no darwin slices built" >&2
		rm -rf "$app"
		return
	fi
	lipo -create -output "$app/Contents/MacOS/portcloak" "${slices[@]}"
	rm -f "${slices[@]}"
	chmod +x "$app/Contents/MacOS/portcloak"
	echo "    $(lipo -archs "$app/Contents/MacOS/portcloak")"

	sed -e "s|{{VERSION}}|$version|g" -e "s|{{BUILD}}|$commit|g" \
		build/darwin/Info.plist >"$app/Contents/Info.plist"
	cp build/darwin/PortCloak.icns "$app/Contents/Resources/PortCloak.icns"
	# Section 4(d) of the Apache License requires a redistribution to carry
	# NOTICE with it, and a .app is a redistribution. Resources/ is where a
	# macOS bundle keeps them.
	cp LICENSE NOTICE "$app/Contents/Resources/"
	printf 'APPL????' >"$app/Contents/PkgInfo"

	# Ad-hoc, NOT a Developer ID signature: it will not pass Gatekeeper on
	# another machine. It is here because an unsigned bundle on Apple silicon
	# is killed on launch rather than warned about, so without it the artifact
	# cannot even be smoke-tested locally.
	#
	# Skipped when staging for a release, because the next thing to touch this
	# bundle is a real Developer ID signature and an ad-hoc one would only be
	# thrown away. Applying it anyway is harmless but misleading in the log.
	if [ "$do_archive" -eq 1 ] && have codesign; then
		codesign --force --deep --sign - "$app" 2>/dev/null &&
			echo "    ad-hoc signed (NOT a Developer ID signature)"
	fi
}

archive_darwin() {
	local out="$stage/macos-universal"
	[ -d "$out/PortCloak.app" ] || return 0
	# `ditto` rather than `zip`: it is the archiver Apple's own notarisation
	# documentation specifies, and the only one that preserves the symlinks and
	# extended attributes a signed bundle depends on. A `zip`-built archive can
	# notarise and then fail to validate once expanded.
	if have ditto; then
		(cd "$out" && ditto -c -k --keepParent PortCloak.app \
			"$dist/PortCloak-$version-macos-universal.zip")
	else
		(cd "$out" && zip -qry "$dist/PortCloak-$version-macos-universal.zip" PortCloak.app)
	fi
	rm -rf "$out"
}

# =========================================================================
# Windows — one .exe per arch, cross-compiled, no C toolchain needed
# =========================================================================
build_windows() {
	# The icon, version block and application manifest are linked in from a
	# .syso the Go toolchain picks up automatically from the main package's
	# directory, by GOARCH. Without go-winres the build still succeeds and the
	# executable is simply generic-looking, which is said out loud rather than
	# failing the release.
	local winres=""
	if have go-winres; then
		winres=go-winres
	elif [ -x "$(go env GOPATH)/bin/go-winres" ]; then
		winres="$(go env GOPATH)/bin/go-winres"
	fi
	if [ -n "$winres" ]; then
		(cd cmd/portcloak && "$winres" make --in "$root/build/windows/winres.json" \
			--product-version "$version" --file-version "$version" \
			--arch amd64,arm64 >/dev/null)
		echo "==> windows resources (icon, version block, manifest)"
	else
		echo "!! go-winres not found — the .exe will have no icon and no version block" >&2
		echo "   go install github.com/tc-hib/go-winres@latest" >&2
	fi

	for arch in amd64 arm64; do
		echo "==> windows/$arch"
		local out="$stage/windows-$arch"
		rm -rf "$out"
		mkdir -p "$out"
		# -H windowsgui: without it Windows opens a console behind the app.
		# CGO_ENABLED=0: Wails' Windows backend is pure Go, so this needs no
		# mingw and cross-compiles from macOS or Linux unchanged.
		GOOS=windows GOARCH="$arch" CGO_ENABLED=0 \
			go build -tags production -trimpath \
			-ldflags "$ldflags_common -H windowsgui" \
			-o "$out/PortCloak.exe" ./cmd/portcloak
		# A bare .exe has nowhere to carry a NOTICE, so the zip is the unit of
		# redistribution and the licence rides beside the binary in it.
		cp LICENSE NOTICE "$out/"
	done
	rm -f cmd/portcloak/*.syso
}

archive_windows() {
	local arch out
	for arch in amd64 arm64; do
		out="$stage/windows-$arch"
		[ -f "$out/PortCloak.exe" ] || continue
		(cd "$out" && zip -qr "$dist/PortCloak-$version-windows-$arch.zip" .)
		rm -rf "$out"
	done
}

# =========================================================================
# Linux — one tarball per arch
# =========================================================================
stage_linux() { # arch binary
	local arch="$1" bin="$2"
	local out="$stage/linux-$arch"
	local tree="$out/portcloak-$version-linux-$arch"
	rm -rf "$out"
	mkdir -p "$tree/bin" "$tree/share/applications"
	cp "$bin" "$tree/bin/portcloak"
	chmod +x "$tree/bin/portcloak"
	cp build/linux/portcloak.desktop "$tree/share/applications/portcloak.desktop"
	for px in 16 32 48 64 128 256 512; do
		mkdir -p "$tree/share/icons/hicolor/${px}x${px}/apps"
		cp "build/linux/appicon-${px}.png" "$tree/share/icons/hicolor/${px}x${px}/apps/portcloak.png"
	done
	cp LICENSE NOTICE "$tree/"
	cp README.md CHANGELOG.md "$tree/" 2>/dev/null || true
}

archive_linux() {
	local arch out tree
	for arch in amd64 arm64; do
		out="$stage/linux-$arch"
		tree="portcloak-$version-linux-$arch"
		[ -d "$out/$tree" ] || continue
		(cd "$out" && tar czf "$dist/$tree.tar.gz" "$tree")
		rm -rf "$out"
	done
}

build_linux() {
	# On a Linux host the default is a native build. It is far faster, and the
	# host's own toolchain is what a developer there wants.
	#
	# It is also what the release uses, on `ubuntu-22.04` runners chosen for
	# their glibc rather than taken as `ubuntu-latest`. A native build inherits
	# the host's glibc and GTK, so the host is the floor: 22.04 carries glibc
	# 2.35, older than the Dockerfile's Debian 12, and `ubuntu-latest` carries
	# 2.39, which would not start on either. The release asserts the resulting
	# ceiling rather than trusting the runner label to stay put.
	#
	# --linux-docker forces the container anyway. That is how a Mac produces a
	# Linux binary at all — there is no native path there — and it is the
	# fallback if the runner images ever stop offering a distribution old
	# enough to build against.
	if [ "$host" = "Linux" ] && [ "$linux_docker" -eq 0 ]; then
		local arch
		arch=$(go env GOARCH)
		case " $linux_arches " in *" $arch "*) ;; *)
			echo "!! skipping linux: --linux-arch asked for $linux_arches, this host is $arch" >&2
			return ;;
		esac
		echo "==> linux/$arch (native)"
		# gtk3: Wails defaults to GTK4 + webkitgtk-6.0, which Debian 12 and
		# Ubuntu 22.04 do not carry. See build/linux/Dockerfile.
		CGO_ENABLED=1 go build -tags production,gtk3 -trimpath -ldflags "$ldflags_common" \
			-o "$dist/.pc-linux" ./cmd/portcloak
		stage_linux "$arch" "$dist/.pc-linux"
		rm -f "$dist/.pc-linux"
		# Only worth saying when both were asked for. A caller that named one
		# architecture already knows it is getting one, and on a release runner
		# this line would be a warning about working as instructed.
		if [ "$linux_arches" = "amd64 arm64" ]; then
			echo "!! the other Linux architecture needs Docker or a machine of that arch" >&2
		fi
		return
	fi

	if ! docker info >/dev/null 2>&1; then
		echo "!! skipping linux: needs Docker (see build/linux/Dockerfile for why)" >&2
		return
	fi
	if ! docker buildx version >/dev/null 2>&1; then
		echo "!! skipping linux: needs docker buildx" >&2
		return
	fi

	# Word-split on purpose: --linux-arch takes one architecture, and the
	# default is both.
	# shellcheck disable=SC2086
	for arch in $linux_arches; do
		echo "==> linux/$arch (docker)"
		local out="$dist/.linux-$arch"
		rm -rf "$out"
		# An architecture that does not match the host runs under emulation and
		# is slow — minutes rather than seconds. The way to avoid that is not to
		# drop the container, which is what pins the glibc floor, but to run
		# this on a machine of that architecture and ask for that one alone:
		# `--linux-arch arm64` on an arm64 host is a native container build.
		# The release workflow does exactly that, one runner per architecture.
		if docker buildx build \
			--platform "linux/$arch" \
			--target export \
			--file build/linux/Dockerfile \
			--build-arg "VERSION=$version" \
			--build-arg "COMMIT=$commit" \
			--build-arg "DATE=$date" \
			--output "type=local,dest=$out" \
			. >/dev/null 2>"$dist/.linux-$arch.log"; then
			stage_linux "$arch" "$out/portcloak"
			rm -rf "$out" "$dist/.linux-$arch.log"
		else
			echo "!! linux/$arch failed; last lines of the build log:" >&2
			tail -20 "$dist/.linux-$arch.log" >&2
			rm -rf "$out"
		fi
	done
}

if [ "$do_build" -eq 1 ]; then
	wants darwin && build_darwin
	wants windows && build_windows
	wants linux && build_linux
fi

if [ "$do_archive" -eq 0 ]; then
	echo
	echo "staged in dist/stage/ — not archived:"
	find "$stage" -mindepth 1 -maxdepth 1 -print | sed "s|$dist/|  |"
	echo
	echo "Sign what is there, then run: $0 --version $version --archive-only"
	exit 0
fi

wants darwin && archive_darwin
wants windows && archive_windows
wants linux && archive_linux
rmdir "$stage" 2>/dev/null || true

# ------------------------------------------------------------------- sums
#
# `shasum` is not on a bare Linux runner, where the tool is `sha256sum`. Both
# write the same format, which is what the verification line in the release
# notes depends on.
#
# Two branches rather than one command held in an array: macOS ships bash 3.2,
# where expanding an empty array under `set -u` is itself an unbound-variable
# error. check-traceability.sh documents the same trap for the same reason.
artifacts() {
	find . -maxdepth 1 -type f ! -name SHA256SUMS ! -name '.*' -print0 | sort -z
}
if have shasum; then
	(cd "$dist" && artifacts | xargs -0 shasum -a 256 >SHA256SUMS)
else
	(cd "$dist" && artifacts | xargs -0 sha256sum >SHA256SUMS)
fi

echo
echo "dist/:"
ls -lh "$dist" | grep -v '^total'
echo
echo "SHA256SUMS:"
cat "$dist/SHA256SUMS"
