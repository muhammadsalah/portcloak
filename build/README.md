# Packaging

```bash
./build/package.sh --version 0.0.1          # everything this host can produce
./build/package.sh --targets windows,linux  # or just some of it
```

That is the whole release path. It builds the frontend, compiles each target
with the `production` tag, stamps the build identity, assembles the platform
bundles and writes `dist/` with one `SHA256SUMS` covering all of them.

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
sysroot does, and `build/linux/Dockerfile` is the cheapest correct one. On a
Linux host the script builds natively instead and skips Docker. The
architecture that does not match the host runs under emulation and is slow.

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

Not automated, and not to be automated in this script: it needs credentials that
must not sit in something a pull request can run.

`package.sh` applies an **ad-hoc** signature on macOS. That is not a Developer ID
signature and will not pass Gatekeeper on another machine — it is there because
an unsigned bundle on Apple silicon is killed on launch rather than warned
about, so without it the artifact cannot even be smoke-tested locally.

Before distribution the macOS bundle needs a Developer ID signature and a
notarisation pass, and the Windows build needs an Authenticode signature. Both
are listed in [`spec/rollout/11-release-0.0.1.md`](../spec/rollout/11-release-0.0.1.md)
as release-gate work.

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
