<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Packaging

```bash
./build/package.sh --version 0.0.1          # everything this host can produce
./build/package.sh --targets windows,linux  # or just some of it
```

It builds the frontend, compiles each target with the `production` tag, stamps
the build identity, assembles the platform bundles and writes `dist/` with one
`SHA256SUMS` covering all of them.

That is the whole *build*. It is not the whole release: a published release is
also signed, notarised and attested, which happens in
[`.github/workflows/release.yml`](../.github/workflows/release.yml) on a `v*`
tag — see [Signing and notarisation](#signing-and-notarisation) below. The
workflow drives this same script, so what a developer builds by hand and what
ships differ in signatures and in nothing else.

Five artifacts, six architectures:

| Artifact | Arch | Built how |
|---|---|---|
| `PortCloak-<v>-macos-universal.zip` | arm64 + x86-64 | Native cgo per slice, `lipo`'d into one universal binary inside the `.app`. **macOS host only** — the bundle needs `lipo` and `codesign`. |
| `PortCloak-<v>-windows-amd64.zip` | x86-64 | Cross-compiled from any host. |
| `PortCloak-<v>-windows-arm64.zip` | arm64 | Cross-compiled from any host. |
| `portcloak-<v>-linux-amd64.tar.gz` | x86-64 | Docker. |
| `portcloak-<v>-linux-arm64.tar.gz` | arm64 | Docker. |

**Windows needs no C cross-toolchain.** Wails' Windows backend is pure Go, so
`CGO_ENABLED=0` builds it — no mingw, no sysroot, and it cross-compiles from
macOS or Linux unchanged. The one flag that matters is `-H windowsgui`: without
it the binary is a *console* executable and Windows opens a terminal window
behind the app.

**Linux cannot be cross-compiled with the Go toolchain alone.** Its Wails
backend is cgo over GTK and WebKit — with `CGO_ENABLED=0` the build fails on
undefined `pointer` in `webview_window_linux.go` — so it needs a C compiler
*and* the GTK/WebKit headers and shared libraries for the target architecture.
A cross C compiler on its own (zig cc, say) supplies libc but not those; a
sysroot does, and `build/linux/Dockerfile` is the cheapest correct one — which
is how a Mac produces a Linux binary at all.

On a Linux host the script builds natively and skips Docker, which is far
faster and is what a developer on that machine wants. **The release does the
same**, on `ubuntu-22.04` runners, one per architecture. A native build
inherits the host's glibc and GTK, so the runner image is the compatibility
floor, and that label is chosen rather than inherited: 22.04 carries glibc
2.35, `ubuntu-latest` carries 2.39, and glibc is forward-compatible but not
backward — a binary built on 24.04 will not start on Debian 12 or Ubuntu 22.04.
The release asserts the resulting ceiling rather than trusting the label to
stay available, because GitHub keeps only the latest two Ubuntu images.

`--linux-docker` forces the container on a Linux host too. It is the fallback
if the runner images stop offering a distribution old enough to build against,
and `--linux-arch` builds one architecture rather than both. Under Docker, an
architecture that does not match the host runs under emulation and is slow —
which is why the release stopped doing that and put each architecture on a
machine of its own.

## The gtk3 build tag

Linux builds pass `-tags production,gtk3`. This is a compatibility decision, not
a preference: Wails v3 defaults to **GTK4 + webkitgtk-6.0**, and the `gtk3` tag
selects **GTK3 + webkit2gtk-4.1** instead. The GTK4 stack is not on Debian 12 or
Ubuntu 22.04, so a binary built against it would refuse to start for most people
likely to run this.

Three places have to agree, or the build fails on a missing `webkitgtk-6.0.pc`:
the tag, the `-dev` packages in `build/linux/Dockerfile`, and the packages and
tags in `.github/workflows/ci.yml`.

Confirm it took by checking what the binary actually asks for — it should name
`libgtk-3.so.0` and `libwebkit2gtk-4.1.so.0`:

```bash
readelf -d portcloak | grep NEEDED
```

For a development build, keep using `go build`:

```bash
npm --prefix frontend ci
npm --prefix frontend run build
go build -o portcloak ./cmd/portcloak
```

A checked-in placeholder in `frontend/dist/` keeps `go build ./...` working on a
machine that has never run npm. The binary then serves an empty asset tree —
a broken UI, but a correct compile, which is what keeps `go test ./internal/...`
runnable without a Node toolchain. `package.sh` refuses to package that state.

## What the `production` tag changes

It is not a flag on one build; it selects different source files, in Wails and
here.

| | development | `-tags production` |
|---|---|---|
| Web inspector | available | the C functions that enable it are not linked in |
| Right-click menu | the webview's own | disabled |
| Developer menu | Reload, Force Reload, Toggle DevTools | not compiled |
| Asset-server logging | every request | off |
| Wails log level | info | warn |

The middle column is the default, so a plain `go build` is a development build.
That is deliberate: the dangerous mistake is shipping a debug build, not
debugging a shipped one, so the release path is the one that has to be asked
for by name.

`nm` on the two binaries is how to confirm it — a production build has no
`_openDevTools` or `_windowEnableDevTools` symbol, only the empty Go stubs Wails
compiles in their place.

## The menu

Leaving the Wails menu unset does not mean "no menu": it installs
`DefaultApplicationMenu()`, which carries a View menu with Reload and Toggle
DevTools, and a Help menu whose only entry navigates the **main window** to
`https://wails.io` with no way back. `internal/app/menu.go` replaces it with the
App, Edit and Window menus macOS genuinely needs. Windows and Linux show no menu
bar at all, because the window leaves `UseApplicationMenu` false.

The Edit menu is not decoration. A WKWebView takes `Cmd+C`, `Cmd+V` and `Cmd+A`
from the first responder chain through those very items, so dropping it breaks
copy and paste in every text field in the app.

## Icons

Sources are the two SVGs in `icons/`, both built from the same geometry as
`assets/logo/mark.svg`:

| File | Plate |
|---|---|
| `appicon-macos.svg` | 824×824 squircle inset in a 1024 canvas — the clear space macOS reserves for its own shadow. Drawing it full-bleed is why most third-party Mac icons look oversized in the Dock. |
| `appicon-square.svg` | full-bleed, for Windows and Linux, which draw into the whole box. |

`./build/icons/generate.sh` renders every raster from those two and writes
`darwin/PortCloak.icns`, `windows/appicon.ico`, `linux/appicon-*.png` and
`internal/app/appicon.png`. The outputs are committed, so no build machine needs
librsvg or ImageMagick; run the script only when the mark changes, then commit
what it produces.

## Signing and notarisation

Automated, in [`.github/workflows/release.yml`](../.github/workflows/release.yml), and
still not in `package.sh` — for the reason it never was: the credentials must
not sit in something a pull request can run. The workflow has them; the script
does not, and can still be run by anyone.

A signature goes on the bundle, not on the zip around it, so `package.sh` has a
seam in the middle:

```bash
./build/package.sh --version 0.0.1 --targets darwin --stage-only   # assemble
#   ... sign dist/stage/macos-universal/PortCloak.app here ...
./build/package.sh --version 0.0.1 --targets darwin --archive-only # then wrap
```

Run without those flags it does both passes back to back, which is what a
developer building locally wants. `--linux-docker` is the third flag the
release needs: on a Linux host the script prefers a fast native build, and a
release wants the container's controlled sysroot for both architectures
instead.

### What each platform gets, and why

| | Signature | Without it |
|---|---|---|
| macOS | Developer ID + hardened runtime + notarised + stapled | Gatekeeper **refuses** the bundle on Apple silicon rather than warning — the app is killed on launch |
| Windows | Authenticode, via [SignPath Foundation](https://signpath.org) | SmartScreen calls the download unrecognised until enough people install it anyway |
| Linux | none native | nothing to satisfy; the platform has no equivalent |
| all | `cosign` over `SHA256SUMS`, plus a build-provenance attestation per artifact | no way to tie a download to this repository at this commit |

The last row is the one that does the most work. A Developer ID signature says
Apple knows who paid for the certificate; an Authenticode signature says the
same about a CA. Neither says the bytes came from this source tree. The Sigstore
signature and the provenance attestation do, and they cover the Linux tarballs
as well — which is why Linux gets no platform-specific scheme of its own. It is
also **keyless**: the identity is the release workflow's OIDC token, so there is
no private key to store, rotate, or leak. Verifying a release:

```bash
sha256sum -c SHA256SUMS

cosign verify-blob --bundle SHA256SUMS.cosign.bundle \
  --certificate-identity 'https://github.com/<owner>/portcloak/.github/workflows/release.yml@refs/tags/v0.0.1' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  SHA256SUMS

gh attestation verify PortCloak-0.0.1-macos-universal.zip --repo <owner>/portcloak
```

`package.sh` still applies an **ad-hoc** signature on macOS when it archives in
one pass. That is not a Developer ID signature and will not pass Gatekeeper on
another machine — it is there because an unsigned bundle on Apple silicon is
killed on launch rather than warned about, so without it the artifact cannot
even be smoke-tested locally. Under `--stage-only` it is skipped, because the
next thing to touch that bundle is a real signature.

### The macOS entitlements

`darwin/entitlements.plist` carries two keys and no comments. The comments are
here instead because **AMFI's plist parser rejects XML comments** — `plutil
-lint` accepts the file and `codesign` then fails with `AMFIUnserializeXML:
syntax error`, naming the first line of the comment.

| Key | Why |
|---|---|
| `com.apple.security.cs.allow-jit` | JavaScriptCore compiles to native code at runtime. Without it the webview loads and every script fails at the point JIT is needed — a window that draws and does nothing. |
| `com.apple.security.cs.allow-unsigned-executable-memory` | The same requirement one layer down: JavaScriptCore also maps writable-executable pages outside the JIT entitlement's scope, and WKWebView crashes on launch under a hardened runtime without it. |

Three things commonly pasted into Wails entitlements are deliberately absent.
**App Sandbox**, because PortCloak reads a Keycloak installation wherever the
operator points it and writes bundles where they choose — a sandbox opened wide
enough for that asserts a containment that does not exist.
**`disable-library-validation`**, because nothing here loads a third-party
plug-in, and enabling it would let any library be loaded into the process
holding the operator's realm secrets. **Network entitlements**, because those
are sandbox keys and outside the sandbox they restrict nothing; listing them
would only imply a control that is not in force.

### What has to exist before any of it runs

Both signing stages are inert until configured, and say so with a workflow
warning rather than failing. The release still builds, checksums and attests.

**Apple** — repository *secrets*:

| Secret | What it is |
|---|---|
| `MACOS_CERTIFICATE_P12` | Developer ID Application certificate and key, exported as `.p12`, then `base64` encoded |
| `MACOS_CERTIFICATE_PASSWORD` | the password set when exporting that `.p12` |
| `MACOS_SIGNING_IDENTITY` | the identity string, e.g. `Developer ID Application: Your Name (TEAMID)` |
| `APPLE_API_KEY_P8` | App Store Connect API key `.p8`, `base64` encoded |
| `APPLE_API_KEY_ID` | that key's Key ID |
| `APPLE_API_ISSUER_ID` | the Issuer ID from App Store Connect → Users and Access → Integrations |

```bash
base64 -i DeveloperID.p12 | pbcopy      # macOS
security find-identity -v -p codesigning # to read the identity string exactly
```

The API key is used rather than an Apple ID and app-specific password because
it is scoped to notarisation and can be revoked on its own, without touching
the account.

**Windows** — one *secret*, `SIGNPATH_API_TOKEN`, and four repository
*variables*: `SIGNPATH_ORGANIZATION_ID`, `SIGNPATH_PROJECT_SLUG`,
`SIGNPATH_SIGNING_POLICY_SLUG` and `SIGNPATH_ARTIFACT_CONFIGURATION_SLUG`.
`SIGNPATH_CONNECTOR_URL` is optional and defaults to the hosted connector. The
organisation ID is a *variable* rather than a secret on purpose: the workflow
tests it to decide whether signing is configured, and `if:` cannot read a
secret.

Applying to SignPath Foundation is described in
[`../spec/rollout/11-release-0.0.1.md`](../spec/rollout/11-release-0.0.1.md).

## Windows resources

The `.exe` icon, the version block and the application manifest come from
`windows/winres.json`, turned into a `.syso` by
[`go-winres`](https://github.com/tc-hib/go-winres) at package time:

```bash
go install github.com/tc-hib/go-winres@latest
```

Without it the build still succeeds and the executable has a generic icon and no
version metadata, which `package.sh` says out loud rather than failing. The
manifest also sets per-monitor-v2 DPI awareness and long-path support; without
the first, the UI is bitmap-scaled and blurry on a high-DPI display.

To check what actually landed in an `.exe`, the manifest is plain XML inside it:

```bash
python3 -c "d=open('PortCloak.exe','rb').read(); i=d.find(b'<assembly'); print(d[i:d.find(b'</assembly>',i)+11].decode())"
```
