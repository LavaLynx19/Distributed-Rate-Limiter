# Build from the repo root so the shared lua/ directory is in context:
#   docker build -f go/Dockerfile .

FROM golang:1.26 AS build
WORKDIR /src

# Dependency layer first, so code edits don't re-download modules.
COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/keyctl ./cmd/keyctl

# Static binary: no libc needed, so the runtime base has no bearing on
# performance. distroless keeps it to a few MB, non-root, with no shell.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/gateway /app/gateway
# Registry provisioning CLI: docker compose exec go /app/keyctl ...
COPY --from=build /out/keyctl /app/keyctl
COPY lua/ /app/lua/

ENV LUA_DIR=/app/lua \
    PORT=3001
EXPOSE 3001
USER nonroot:nonroot
ENTRYPOINT ["/app/gateway"]
