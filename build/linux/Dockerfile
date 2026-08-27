# Linux build environment.
#
# Linux is the one target that cannot be cross-compiled from macOS with the Go
# toolchain alone. Wails' Linux backend is cgo over GTK3 and WebKit2GTK — with
# CGO_ENABLED=0 the build fails outright on undefined `pointer` in
# webview_window_linux.go — so producing a Linux binary needs a C compiler *and*
# the GTK and WebKit development headers and shared libraries for the target
# architecture. A cross C compiler alone (zig cc, for instance) does not supply
# those; a sysroot does, and this image is the cheapest correct sysroot.
#
# Driven by build/package.sh. Not used on a Linux host, which builds natively.
FROM golang:1.27-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
        libgtk-3-dev \
        libwebkit2gtk-4.1-dev \
        pkg-config \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY . .

ARG VERSION=0.0.0-unknown
ARG COMMIT=unknown
ARG DATE=unknown

# gtk3 is not a preference, it is a compatibility decision. Wails v3 defaults
# to GTK4 + webkitgtk-6.0; the gtk3 tag selects GTK3 + webkit2gtk-4.1 instead.
# The GTK4 stack is not present on Debian 12 or Ubuntu 22.04, so a binary built
# against it would refuse to start for most of the people likely to run this.
# The tag has to match the -dev packages installed above, and both have to match
# what CI installs — see .github/workflows/ci.yml.
#
# production keeps the inspector out; trimpath keeps the builder's paths out.
RUN CGO_ENABLED=1 go build -tags production,gtk3 -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
        -o /out/portcloak ./cmd/portcloak

# A scratch stage so `--output type=local` extracts the binary and nothing else.
FROM scratch AS export
COPY --from=build /out/portcloak /portcloak
