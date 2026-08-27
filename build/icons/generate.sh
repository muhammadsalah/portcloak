#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0

#
# Regenerates every raster icon from the two SVG sources in this directory.
#
# The outputs are committed, so a build machine needs neither rsvg-convert nor
# ImageMagick. Run this only when the mark itself changes, then commit what it
# produces — that is the trade that keeps CI free of an image toolchain while
# still letting the icons be derived rather than hand-drawn.
#
#   ./build/icons/generate.sh
#
# Requires: rsvg-convert (librsvg), png2ico or ImageMagick, and on macOS
# iconutil. Missing tools are reported and skipped rather than failing the run,
# because a Linux contributor cannot produce an .icns and should not be blocked.

set -euo pipefail

cd "$(dirname "$0")"
root=$(cd ../.. && pwd)

macos_svg="appicon-macos.svg"
square_svg="appicon-square.svg"

out_darwin="$root/build/darwin"
out_windows="$root/build/windows"
out_linux="$root/build/linux"
mkdir -p "$out_darwin" "$out_windows" "$out_linux"

have() { command -v "$1" >/dev/null 2>&1; }

if ! have rsvg-convert; then
	echo "rsvg-convert not found (brew install librsvg / apt install librsvg2-bin)" >&2
	exit 1
fi

render() { # svg size out
	rsvg-convert --width="$2" --height="$2" --output="$3" "$1"
}

# ---------------------------------------------------------------- macOS .icns
#
# iconutil wants an .iconset directory whose filenames encode both the point
# size and the scale factor. The 1x/2x pairs are not redundant: macOS picks by
# point size and then by the display's scale, so omitting a 2x rung makes the
# icon soft on a Retina display rather than making it fall back gracefully.
if have iconutil; then
	set="PortCloak.iconset"
	rm -rf "$set" && mkdir -p "$set"
	for pt in 16 32 128 256 512; do
		render "$macos_svg" "$pt" "$set/icon_${pt}x${pt}.png"
		render "$macos_svg" "$((pt * 2))" "$set/icon_${pt}x${pt}@2x.png"
	done
	iconutil --convert icns --output "$out_darwin/PortCloak.icns" "$set"
	rm -rf "$set"
	echo "wrote build/darwin/PortCloak.icns"
else
	echo "iconutil not found (macOS only) — skipping .icns" >&2
fi

# ------------------------------------------------------------- Windows .ico
#
# One .ico carries every size Explorer, the taskbar and Alt-Tab ask for. 256 is
# stored as PNG inside the container, which is what keeps the file small.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for px in 16 24 32 48 64 128 256; do
	render "$square_svg" "$px" "$tmp/$px.png"
done
if have magick; then
	magick "$tmp"/{16,24,32,48,64,128,256}.png "$out_windows/appicon.ico"
	echo "wrote build/windows/appicon.ico"
elif have convert; then
	convert "$tmp"/{16,24,32,48,64,128,256}.png "$out_windows/appicon.ico"
	echo "wrote build/windows/appicon.ico"
else
	echo "ImageMagick not found — skipping .ico" >&2
fi

# ----------------------------------------------------------------- Linux PNG
#
# hicolor is the theme every desktop environment falls back to, so shipping
# these sizes means the icon resolves without the app having to register a
# theme of its own.
for px in 16 32 48 64 128 256 512; do
	render "$square_svg" "$px" "$out_linux/appicon-${px}.png"
done
echo "wrote build/linux/appicon-{16,32,48,64,128,256,512}.png"

# ------------------------------------------------- embedded about-box icon
#
# application.Options.Icon is what the About dialog and, on Linux, the window
# manager display. 512 is the largest size any of them ask for.
render "$square_svg" 512 "$root/internal/app/appicon.png"
echo "wrote internal/app/appicon.png"
