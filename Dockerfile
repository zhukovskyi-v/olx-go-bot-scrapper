# syntax=docker/dockerfile:1.7

# Build stage
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Cache modules layer separately from source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO disabled: libsql-client-go is pure Go, so we can produce a static binary
# that runs on a scratch/distroless base.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

# Runtime stage — distroless static carries ca-certificates (needed for HTTPS
# to OLX and Telegram) and runs as non-root by default.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/bot /app/bot

# Cloud-friendly defaults:
#   TOKEN  — Telegram bot token (set at deploy time, required).
#   DB_URL — `libsql://<host>?authToken=<t>` (recommended) or
#            `file:/data/olx.db` if you mount a volume at /data.
#   ENV    — leave unset for text logs to stdout (Cloud Run / Fly / Railway
#            collect stdout). Avoid `prod` here: it writes to ./slog.log, which
#            is invisible to log aggregators and lost on container restart.
ENV ENV=""

# Volume hook for file:-backed SQLite deployments. Harmless if you use Turso.
VOLUME ["/data"]

ENTRYPOINT ["/app/bot"]
