# ==============================================================================
# RTVortex — Multi-stage Docker Build
# ==============================================================================
# Architecture:
#   - Go API Server:  REST API on port 8080, connects to Engine via gRPC
#   - C++ Engine:     gRPC server on port 50051 (separate container or sidecar)
#
# Build from the mono/ directory:
#   docker build -f deploy/docker/Dockerfile -t rtvortex-server .
# ==============================================================================

# =============================================================================
# Stage 1: Build Go Server
# =============================================================================
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Cache dependencies first
COPY server-go/go.mod server-go/go.sum ./server-go/
WORKDIR /build/server-go
RUN go mod download

# Copy source and build
COPY server-go/ ./

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /build/bin/RTVortexGo \
    ./cmd/rtvortex-server/

# =============================================================================
# Stage 2: Runtime
# =============================================================================
FROM alpine:3.19

LABEL org.opencontainers.image.title="RTVortex Server"
LABEL org.opencontainers.image.description="RTVortex AI code review API server (Go)"
LABEL org.opencontainers.image.source="https://github.com/AuralithAI/RT-AI-PR-Reviewer"
LABEL org.opencontainers.image.vendor="AuralithAI"

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S rtvortex && \
    adduser -S -G rtvortex rtvortex

WORKDIR /app

# Copy binary
COPY --from=builder /build/bin/RTVortexGo /app/RTVortexGo

# Copy SQL files for schema bootstrap
COPY --from=builder /build/server-go/db/sql /app/db/sql
COPY --from=builder /build/server-go/db/migrations /app/db/migrations

# Copy config and schema files
COPY config/ /app/config/
COPY schemas/ /app/schemas/
COPY prompting/ /app/prompting/

# Create data directories
RUN mkdir -p /app/temp /app/data /app/config && \
    chown -R rtvortex:rtvortex /app

ENV RTVORTEX_HOME=/app

USER rtvortex

# Expose REST API port
EXPOSE 8080

# Health check against the Go server /health endpoint
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -sf http://localhost:8080/health || exit 1

# Default environment
ENV ENGINE_HOST="engine"
ENV ENGINE_PORT="50051"
ENV ENGINE_NEGOTIATION="PLAINTEXT"

ENTRYPOINT ["/app/RTVortexGo"]
