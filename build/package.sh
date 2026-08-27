#!/usr/bin/env bash
#
# Builds a distributable PortCloak for the host platform.
#
#   ./build/package.sh                 # version from git describe
#   ./build/package.sh --version 0.0.1
#   ./build/package.sh --skip-frontend # reuse an existing frontend/dist
#
# Output lands in dist/ together with SHA256SUMS.
#
# What makes these different from `go build ./cmd/portcloak`:
#
#   * -tags production. This is the switch Wails reads to compile out the
#     inspector, and that PortCloak reads to drop the Developer menu and
#     disable the webview's context menu. A release built without it ships a
#     right-click "Inspect Element" to every user.
#   * -trimpath and -ldflags "-s -w". Local absolute paths are removed from the
#     binary and the symbol table is dropped, which is both smaller and one
#     fewer thing leaking about the machine that built it.
#   * version, commit and build date stamped in, because the release gate
#     requires the app to be able to say which build it is.
#
# Signing and notarisation are deliberately NOT here: they need credentials
# that must not sit in a script a pull request can run. See the release gate in
# spec/rollout/11-release-0.0.1.md.

set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

version=""
skip_frontend=0
while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		version="$2"
		shift 2
		;;
	--skip-frontend)
		skip_frontend=1
		shift
		;;
	-h | --help)
		sed -n '2,28p' "$0"
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 2
		;;
	esac
done

# A tag if we are on one, otherwise the nearest tag plus the distance and hash.
if [ -z "$version" ]; then
	version=$(git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-unknown")
	version=${version#spec-}
	version=${version#v}
fi

commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
	commit="${commit}-dirty"
fi
# SOURCE_DATE_EPOCH, when the caller sets it, makes the stamp reproducible.
date=$(date -u -r "${SOURCE_DATE_EPOCH:-$(date +%s)}" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
	date -u +%Y-%m-%dT%H:%M:%SZ)

echo "PortCloak $version ($commit) $date"

dist="$root/dist"
rm -rf "$dist"
mkdir -p "$dist"

# ------------------------------------------------------------------ frontend
#
# The Go binary embeds frontend/dist. Building the app without rebuilding the
# frontend first is the one mistake that produces a release which looks fine
# and runs last week's UI, so it takes an explicit flag to skip.
if [ "$skip_frontend" -eq 0 ]; then
	echo "==> frontend"
	npm --prefix frontend ci
	npm --prefix frontend run build
else
	echo "==> frontend (skipped)"
fi

if [ ! -f frontend/dist/index.html ]; then
	echo "frontend/dist/index.html is missing — refusing to package a binary with no UI" >&2
	exit 1
fi

ldflags="-s -w"
ldflags="$ldflags -X main.version=$version"
ldflags="$ldflags -X main.commit=$commit"
ldflags="$ldflags -X main.date=$date"

build_binary() { # goos goarch output
	echo "==> go build $1/$2"
	GOOS="$1" GOARCH="$2" CGO_ENABLED=1 \
		go build -tags production -trimpath -ldflags "$ldflags" -o "$3" ./cmd/portcloak
}

sums() { # writes SHA256SUMS next to the artifacts
	(cd "$dist" && find . -maxdepth 1 -type f ! -name SHA256SUMS -print0 |
		sort -z | xargs -0 shasum -a 256 >SHA256SUMS)
	echo "==> dist/SHA256SUMS"
	cat "$dist/SHA256SUMS"
}

case "$(uname -s)" in

# ------------------------------------------------------------------- macOS
Darwin)
	app="$dist/PortCloak.app"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

	# A universal binary is one file that runs natively on both Apple silicon
	# and Intel. Cross-compiling the other arch needs cgo, which needs the
	# matching SDK slice; if it is not there we ship the host arch alone rather
	# than failing, and say so.
	arm="$dist/.portcloak-arm64"
	amd="$dist/.portcloak-amd64"
	build_binary darwin arm64 "$arm"
	if build_binary darwin amd64 "$amd" 2>/dev/null; then
		lipo -create -output "$app/Contents/MacOS/portcloak" "$arm" "$amd"
		echo "==> universal binary (arm64 + amd64)"
	else
		cp "$arm" "$app/Contents/MacOS/portcloak"
		echo "!! amd64 slice could not be built; shipping arm64 only" >&2
	fi
	rm -f "$arm" "$amd"
	chmod +x "$app/Contents/MacOS/portcloak"

	sed -e "s|{{VERSION}}|$version|g" -e "s|{{BUILD}}|$commit|g" \
		build/darwin/Info.plist >"$app/Contents/Info.plist"
	cp build/darwin/PortCloak.icns "$app/Contents/Resources/PortCloak.icns"
	printf 'APPL????' >"$app/Contents/PkgInfo"

	# An ad-hoc signature is not a Developer ID signature and will not pass
	# Gatekeeper on another machine. It is here because an unsigned bundle on
	# Apple silicon is killed on launch rather than merely warned about, so
	# without this the artifact cannot even be smoke-tested locally.
	if command -v codesign >/dev/null 2>&1; then
		codesign --force --deep --sign - "$app" 2>/dev/null &&
			echo "==> ad-hoc signed (NOT a Developer ID signature)"
	fi

	(cd "$dist" && zip -qry "PortCloak-$version-macos.zip" PortCloak.app && rm -rf PortCloak.app)
	sums
	;;

# ------------------------------------------------------------------- Linux
Linux)
	stage="$dist/portcloak-$version"
	mkdir -p "$stage/bin" "$stage/share/applications"
	build_binary linux "$(go env GOARCH)" "$stage/bin/portcloak"
	cp build/linux/portcloak.desktop "$stage/share/applications/portcloak.desktop"
	for px in 16 32 48 64 128 256 512; do
		d="$stage/share/icons/hicolor/${px}x${px}/apps"
		mkdir -p "$d"
		cp "build/linux/appicon-${px}.png" "$d/portcloak.png"
	done
	cp README.md CHANGELOG.md "$stage/" 2>/dev/null || true
	(cd "$dist" && tar czf "portcloak-$version-linux-$(go env GOARCH).tar.gz" "portcloak-$version" &&
		rm -rf "portcloak-$version")
	sums
	;;

# ----------------------------------------------------------------- Windows
MINGW* | MSYS* | CYGWIN*)
	# The icon and the version block are linked in from a .syso, which the Go
	# toolchain picks up automatically from the package directory. go-winres
	# generates it from build/windows/winres.json.
	if command -v go-winres >/dev/null 2>&1; then
		(cd cmd/portcloak && go-winres make --in "$root/build/windows/winres.json" \
			--product-version "$version" --file-version "$version")
	else
		echo "!! go-winres not found — the .exe will have no icon and no version block" >&2
		echo "   go install github.com/tc-hib/go-winres@latest" >&2
	fi
	build_binary windows "$(go env GOARCH)" "$dist/PortCloak.exe"
	rm -f cmd/portcloak/*.syso
	sums
	;;

*)
	echo "unsupported host platform: $(uname -s)" >&2
	exit 1
	;;
esac

echo
echo "dist/:"
ls -la "$dist"
