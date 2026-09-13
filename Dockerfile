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

# CGO disabled: the libsql *remote* driver is pure Go, so we can produce a static
# binary that runs on a scratch/distroless base. The flip side: no local sqlite
# driver is linked in, so DB_URL must be a libsql:// URL (see below).
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

# Runtime stage — distroless static carries ca-certificates (needed for HTTPS
# to OLX and Telegram) and runs as non-root by default.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/bot /app/bot

# Cloud-friendly defaults:
#   TOKEN  — Telegram bot token (set at deploy time, required).
#   DB_URL — `libsql://<host>?authToken=<t>` (Turso). A `file:` DSN does NOT
#            work in this image: libsql-client-go hands local files to a sqlite
#            driver, and none is linked in (CGO off, no driver import). It fails
#            at startup with "no sqlite driver present".
#   ENV    — `prod` for structured JSON logs on stdout, unset for text logs on
#            stdout. Either is collected by Cloud Run / Fly / Railway.
ENV ENV=""

# No volume is required: state lives in the remote libsql database.

ENTRYPOINT ["/app/bot"]
