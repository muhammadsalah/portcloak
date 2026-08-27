# Packaging

```bash
./build/package.sh --version 0.0.1
```

That is the whole release path. It builds the frontend, compiles with the
`production` tag, stamps the build identity, assembles the platform bundle for
the host OS and writes `dist/` with a `SHA256SUMS` beside the artifact.

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
