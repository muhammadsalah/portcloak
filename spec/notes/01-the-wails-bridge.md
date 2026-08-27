# 01 — The Wails bridge

Everything between `internal/app` and `frontend/src`. Nothing on this seam is checked by either
compiler: Go marshals a struct, the frontend reads property names off a value it was handed, and
the only thing connecting the two is that both were written by someone who believed the same
thing. Every entry below is a case where they believed different things and nothing said so.

---

## 1. A nil slice reaches the frontend as `null`, not `[]`

**Symptom** — On a fresh install the app never gets past its loading spinner. "Loading
configuration…" on Environments and Storage, "Reading storage…" on Snapshots. No error dialog,
no failure notice; the window looks hung. Adding an environment by hand to `config.yaml` makes
it go away, which is why this survives all manual testing after the first five minutes.

**Cause** — `encoding/json` writes a nil slice as `null` and a nil map as `null`. A Go slice
that was never appended to is nil, so an unconfigured PortCloak answered
`ConfigController.Load` with `{"environments": null, "storage": null}`. The first thing every
view does with a list is read its length or iterate it, and that throws — *after* the spinner
went up and *before* anything replaced it. The rejected promise was discarded by a bare `void`,
so the console had a `TypeError` and the screen had a spinner forever.

**Rule** — A list-valued field crosses the bridge as `[]` and a map as `{}`, always. This is
not enforced at construction sites — the fields that hurt most are nested ones nobody remembers,
a job's `phases`, a snapshot's `realms` — but at the boundary, by `lists()` in
`internal/app/lists.go`. Any controller method whose response transitively contains a slice or a
map declares a named result and normalises it:

```go
func (c *ConfigController) Load() (res ConfigSnapshot) {
	defer func() { res = lists(res) }()
	...
}
```

The `defer` form is deliberate: it covers early returns on error paths too, and a new `return`
added later cannot forget it.

**Guard** — `TestControllers_NeverHandTheFrontendNull` (`internal/app/empty_config_test.go`)
drives every screen-load method against a `PORTCLOAK_HOME` that has never been written to,
marshals each response and fails on any `null` anywhere in it, naming the path.

---

## 2. A `yaml:` tag does nothing for JSON

**Symptom** — Worse than a crash, because nothing crashes. The environments list renders rows
with blank names, `kindLabel(undefined)`, an empty target line. The data is plainly there — the
list has the right number of rows — but every field reads as `undefined`.

**Cause** — `config.Environment`, `config.Storage` and `config.Preferences` are read from
`config.yaml` and were tagged for YAML only. They also cross the bridge, where they are
marshalled by `encoding/json`, which ignores `yaml:` tags entirely and falls back to the Go
field name. The frontend received `{"Name": "prod", "Kind": "ssh"}` and read `env.name` and
`env.kind`.

The asymmetry is what hides it: *inbound* works. `json.Unmarshal` matches field names
case-insensitively, so a `SaveEnvironment` posting `{"name": …}` populates `Name` correctly.
Saving an environment works; displaying it does not.

**Rule** — Any type that crosses the bridge carries `json:` tags, even when it exists primarily
for another encoding. Both tags, same name:

```go
Name string `yaml:"name" json:"name"`
```

The inline catch-all that preserves unknown keys across versions is `yaml:",inline" json:"-"` —
it is a YAML mechanism and has no meaning to the frontend.

**Guard** — `TestConfigModel_CrossesTheBridgeUnderTheNamesTheFrontendReads`
(`internal/app/empty_config_test.go`) marshals a populated config and asserts the keys the
screens actually read are present, then fails on any key arriving with a capital first letter —
which is exactly what a forgotten `json:` tag looks like from the frontend.

---

## 3. A bound method is addressed by its fully-qualified Go name

**Symptom** — `unknown bound method name`, on every call, from every screen.

**Cause** — Wails keys its binding table on `<package path>.<Type>.<Method>`, and
`Call.ByName` looks that string up verbatim. `"ConfigController.Load"` does not resolve;
`"portcloak/internal/app.ConfigController.Load"` does. The Go package path is part of the
address, not decoration.

What made the wrong form look right was `ServiceName()`. Two controllers implemented it,
returning `"ConfigController"`, under a comment saying it was what the binding layer called
them. It is not — Wails uses it for log lines only, and it is on the internal-methods list, so
it is never bound at all.

**Rule** — The frontend builds every address from the `goPackage` constant in
`frontend/src/api.ts`. Do not reintroduce `ServiceName()`: a method that appears to set an
address and does not is worse than no method.

**Guard** — `TestBindings_EveryFrontendCallResolves` (`internal/app/bindings_test.go`) reads
`api.ts`, extracts every `call<T>("Controller", "Method")`, and resolves each one against the
real controllers by reflection — including that the address is built from the package path and
not merely that the constant is declared. Renaming this Go package now fails the build instead
of the application. `TestBindings_EveryBoundMethodIsReachable` runs the check in reverse and
catches a bound method that nothing calls.

---

## 4. A view that throws leaves its spinner on screen

**Symptom** — The application looks hung rather than broken. See entry 1 for how this turned a
one-line marshalling fault into "the tool does not start".

**Cause** — Every view is `async`: it clears the content pane, appends a spinner, awaits the
engine, then replaces both. If the await or anything after it throws, nothing replaces
anything. `render()` invoked views as `void renderX(content)`, which discards the rejection.

**Rule** — Views are invoked through `show()` in `frontend/src/main.ts`, which catches, replaces
the pane with the failure and its message, and offers "Try again". A view is still expected not
to throw; this exists so that the next one that does is *visible*, in one place, rather than
silent on eight screens.

**Note** — `refreshCounters()` swallows its errors on purpose, and that is fine: it repaints
navigation counts on a five-second interval, and a transient failure there must not put an error
on top of a working screen. It is not a model for the views.

---

## 5. The Vite dev server has to be pinned to IPv4 and to its port

**Symptom** — The app logs `Connected to frontend dev server!` and then fails every asset
request with `[ExternalAssetHandler] Proxy error error=dial tcp4 127.0.0.1:5173: connect:
connection refused`. It says it connected.

**Cause** — Vite binds `::1` only by default. Wails' asset proxy forces IPv4 whenever the
dev-server URL names `localhost` or `127.0.0.1` — a Windows workaround in `assetserver` — while
the startup health check ahead of it uses a plain `http.Client` that is happy over IPv6. The
check passes and the proxy behind it cannot reach the same server.

The second half of the same trap: on a taken port Vite quietly moves to 5174 while the Go side
goes on proxying to 5173.

**Rule** — `frontend/vite.config.ts` pins `server.host` to `127.0.0.1`, `server.port` to 5173
and `server.strictPort` to `true`. Refusing to start is the useful outcome.

**Scope** — `npm run dev` only. The embedded production bundle is built by `vite build`, which
does not read `server` options.

---

## 6. An empty configuration is a screen, not an empty list

**Symptom** — Not a fault, but the reason entry 1 was reported as "the tool screens don't handle
empty configurations". Once the crash was fixed, a first launch showed a list saying
"0 environments" beside a panel saying "Select an environment on the left, or add one."

**Rule** — The state every operator is in exactly once deserves the same care as the states they
are in daily. Environments and Storage each render a dedicated empty state that says what the
thing *is*, what PortCloak does with it, and where the file lives. The Snapshots screen already
did this through `LibraryView.FirstRun`, which is the model.

**Related** — Test against a `PORTCLOAK_HOME` pointing at an empty temporary directory. It is
the only way to see the first launch twice.

---

## 7. A wizard that asks for nothing still sends something

**Symptom** — Restoring an encrypted snapshot fails on the Destination step:

> **The restore did not start**
> This snapshot is encrypted and could not be opened with the key supplied.

No key had been supplied, because no step ever asked for one.

**Cause** — The restore wizard passed `passphrase: ""` and `identities: []` to
`InspectAPI.open` and again to `RestoreAPI.apply`, and rendered no input for either. The
placeholders read as "nothing needed here" rather than as "this is unfinished", and every
unencrypted snapshot restored fine, so the gap only showed on the first encrypted one.

The second half is worse than the missing field: the restore *job* opens the bundle again on
its own (`Orchestrator.runRestore`) rather than reusing the session the wizard opened. A key
collected for the pre-flight check alone would have passed the wizard and then failed at the
point of no return, with the ephemeral clone already created.

**Rule** — A secret the engine will need is collected by the screen that needs it and carried to
**every** call that takes it. Where a flow opens the same bundle twice, it passes the key twice.

The key is asked for on the step rather than in a modal, next to the notice promising that
decryption runs before the destination is contacted — so a wrong key can be corrected beside the
message saying it was wrong. The inspector still asks in a modal, because it is answering "can I
show you this at all" on the way in; both render the same inputs from `views/key.ts`.

**Guard** — `TestRestoreWizard_NeverHardcodesAnEmptyKey` and
`TestSnapshotKey_IsAskedForInOnePlace` (`internal/app/restore_key_test.go`) read the views and
fail on a literal empty key in the restore flow, on a missing key input, and on either screen
open-coding the inputs instead of sharing them.

**Why the engine cannot catch this** — An empty key is a legitimate value at both call sites:
the inspector deliberately tries without one first, so that a snapshot needing no key is never
interrogated for one. Only the caller knows which case it is in, and only the restore flow knows
it from the library listing before either call. That is why the invariant is asserted against
the source rather than in a controller.
