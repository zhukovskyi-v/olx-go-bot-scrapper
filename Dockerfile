# syntax=docker/dockerfile:1.7

# Build stage
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Cache modules layer separately from source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled: libsql-client-go is pure Go, so we can produce a static binary
# that runs on a scratch/distroless base.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
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

# For file:-backed SQLite, attach a Railway Volume mounted at /data and set
# DB_URL=file:/data/olx.db. Otherwise use Turso libsql://... — no volume needed.

ENTRYPOINT ["/app/bot"]
