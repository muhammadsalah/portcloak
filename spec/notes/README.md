# Notes — development gotchas

The rest of `spec/` says what PortCloak is meant to do. This folder says what has actually
gone wrong while building it, and what stops each thing going wrong again.

Everything here is a **fault that reached working code**, not a style preference. Each entry
was found the expensive way — by an operator, by a contract table, or by a screen that never
finished loading — so each one is written to be recognised again from its symptom rather than
from its cause.

## Index

| # | Note | Covers |
|---|------|--------|
| 01 | [`01-the-wails-bridge.md`](./01-the-wails-bridge.md) | Go ↔ frontend: bound method names, `null` lists, struct tags, dev server, views that never finish, secrets never collected, a screen that does not keep up with the run |
| 02 | [`02-the-engine.md`](./02-the-engine.md) | Targets, clones, storage and resilience: silent success, leaked clones, checkpoints, kc.sh option drift and where kc.sh is, pushing into a clone, keys the tool refuses to keep, an index shared between snapshots |

## Entry format

Each entry is four lines of substance:

- **Symptom** — what you see, in the words you would search for.
- **Cause** — the actual mechanism. Not "a bug in X".
- **Rule** — what to do instead, stated so it can be followed without re-deriving the cause.
- **Guard** — the test that fails if it comes back. An entry with no guard is a bug waiting to
  reappear; write the test, then write the entry.

## Adding one

Add a note when a fault took more than a few minutes to understand *and* the understanding does
not live in the code. If a comment at the call site would carry it, put it there instead — the
worst outcome is a folder of true statements nobody reads at the moment they are needed. The
entries here are the ones that span two languages, two processes, or a seam where neither side
can see the other.
