# ---- build ----
# Runs on the build host's native architecture and cross-compiles to the target
# architecture ($TARGETOS/$TARGETARCH are provided by buildx). Because the Go
# toolchain cross-compiles, multi-arch images build fast without QEMU emulating
# the compiler.
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /out/pbs-auth-server .

# ---- runtime (minimal, no shell, no package manager) ----
FROM scratch
COPY --from=build /out/pbs-auth-server /pbs-auth-server
# non-root (nobody)
USER 65534:65534
EXPOSE 8099
ENTRYPOINT ["/pbs-auth-server"]
