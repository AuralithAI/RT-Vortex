# ==============================================================================
# RTVortex API Server — Multi-stage Docker Build
# ==============================================================================

# ── Stage 1: Build ───────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .

ARG VERSION=v0.0.0
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /build/bin/RTVortexGo \
    ./cmd/rtvortex-server/

# ── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.19 AS runtime

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S rtvortex && \
    adduser -S -G rtvortex rtvortex

WORKDIR /app

COPY --from=builder /build/bin/RTVortexGo /app/RTVortexGo
COPY --from=builder /build/db/migrations /app/db/migrations

RUN mkdir -p /app/temp /app/data /app/config && \
    chown -R rtvortex:rtvortex /app

ENV RTVORTEX_HOME=/app

USER rtvortex

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -sf http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/RTVortexGo"]
