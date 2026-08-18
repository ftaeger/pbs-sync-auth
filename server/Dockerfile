# ---- build ----
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/pbs-auth-server .

# ---- runtime (minimal, no shell, no package manager) ----
FROM scratch
COPY --from=build /out/pbs-auth-server /pbs-auth-server
# non-root (nobody)
USER 65534:65534
EXPOSE 8099
ENTRYPOINT ["/pbs-auth-server"]
