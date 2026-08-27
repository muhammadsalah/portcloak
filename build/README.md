# Packaging

`go build ./cmd/portcloak` produces the application, with the frontend embedded.
The frontend has to be built first:

```bash
npm --prefix frontend ci
npm --prefix frontend run build
go build -ldflags "-X main.version=0.0.1" -o portcloak ./cmd/portcloak
```

A checked-in placeholder in `frontend/dist/` keeps `go build ./...` working on a
machine that has never run npm. The binary then serves an empty asset tree —
a broken UI, but a correct compile, which is what keeps `go test ./internal/...`
runnable without a Node toolchain.

## Signing and notarisation

Not automated for 0.0.1. The macOS bundle needs a Developer ID signature and a
notarisation pass before it can be distributed; the Windows build needs an
Authenticode signature. Both are listed in
[`spec/rollout/11-release-0.0.1.md`](../spec/rollout/11-release-0.0.1.md) as
release-gate work rather than build-time work, because they need credentials CI
should not hold on a pull request.
