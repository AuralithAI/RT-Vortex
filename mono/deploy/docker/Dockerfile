# AI PR Reviewer
# Multi-stage build for minimal image size
#
# Architecture:
#   - C++ Engine: gRPC server on port 50051
#   - Java Server: REST/gRPC API on port 8080/9090
#
# The engine is built as a standalone executable and runs as a separate process.

# =============================================================================
# Stage 1: Build C++ Engine (gRPC Server)
# =============================================================================
FROM ubuntu:22.04 AS engine-builder

RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    git \
    pkg-config \
    libssl-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy engine source and proto files
COPY engine/ ./engine/
COPY proto/engine.proto ./proto/

# Build engine with gRPC server
RUN cmake -B build -S engine \
    -DCMAKE_BUILD_TYPE=Release \
    -DAIPR_BUILD_TESTS=OFF \
    -DAIPR_BUILD_SERVER=ON \
    -DAIPR_USE_FAISS=OFF \
    && cmake --build build --config Release --parallel $(nproc)


# =============================================================================
# Stage 2: Build Java Server
# =============================================================================
FROM eclipse-temurin:21-jdk AS server-builder

WORKDIR /build

# Copy Gradle files
COPY settings.gradle build.gradle gradlew VERSION ./
COPY gradle/ ./gradle/
COPY build-logic/ ./build-logic/

# Copy source
COPY server/ ./server/
COPY proto-java/ ./proto-java/

# Make gradlew executable
RUN chmod +x gradlew

# Build (skip engine tasks - it's built separately)
RUN ./gradlew :server:bootJar :proto-java:jar --no-daemon -x buildEngine


# =============================================================================
# Stage 3: Final Runtime Image
# =============================================================================
FROM eclipse-temurin:21-jre-jammy

# Labels
LABEL org.opencontainers.image.title="AI PR Reviewer"
LABEL org.opencontainers.image.description="AI-powered code review server with standalone C++ engine"
LABEL org.opencontainers.image.source="https://github.com/AuralithAI/RT-AI-PR-Reviewer"

# Install runtime dependencies for C++ engine
RUN apt-get update && apt-get install -y \
    libstdc++6 \
    curl \
    netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r aipr && useradd -r -g aipr aipr

WORKDIR /app

# Copy C++ engine executable
COPY --from=engine-builder /build/build/bin/aipr-engine /app/bin/aipr-engine
RUN chmod +x /app/bin/aipr-engine

# Copy Java server JAR
COPY --from=server-builder /build/server/build/libs/*.jar /app/lib/server.jar
COPY --from=server-builder /build/proto-java/build/libs/*.jar /app/lib/

# Copy config files
COPY config/ /app/config/
COPY schemas/ /app/schemas/
COPY prompting/ /app/prompting/

# Create data directories
RUN mkdir -p /app/data /app/logs && chown -R aipr:aipr /app

USER aipr

# Expose ports
# 8080 - REST API
# 9090 - gRPC API (Java server for SDK clients)
# 50051 - Engine gRPC (internal, Java server connects here)
EXPOSE 8080 9090 50051

# Health check (checks Java server)
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8080/actuator/health || exit 1

# Default environment
ENV JAVA_OPTS="-Xms512m -Xmx2g -XX:+UseG1GC"
ENV ENGINE_HOST="localhost"
ENV ENGINE_PORT="50051"
ENV ENGINE_NEGOTIATION="PLAINTEXT"

# Copy startup script
COPY deploy/docker/entrypoint.sh /app/entrypoint.sh

# Entry point starts both engine and server
ENTRYPOINT ["/app/entrypoint.sh"]
