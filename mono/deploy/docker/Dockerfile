# AI PR Reviewer Server
# Multi-stage build for minimal image size

# Stage 1: Build engine
FROM ubuntu:22.04 AS engine-builder

RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY core/engine-cpp/ ./engine-cpp/

RUN cmake -B build -S engine-cpp \
    -DCMAKE_BUILD_TYPE=Release \
    -DAIPR_BUILD_TESTS=OFF \
    && cmake --build build --parallel $(nproc)


# Stage 2: Build server
FROM eclipse-temurin:17-jdk AS server-builder

WORKDIR /build

# Copy Gradle files
COPY settings.gradle build.gradle gradlew ./
COPY gradle/ ./gradle/
COPY build-logic/ ./build-logic/

# Copy source
COPY server/ ./server/

# Build
RUN ./gradlew :server:bootJar --no-daemon


# Stage 3: Final image
FROM eclipse-temurin:17-jre-jammy

# Labels
LABEL org.opencontainers.image.title="AI PR Reviewer"
LABEL org.opencontainers.image.description="AI-powered code review server"
LABEL org.opencontainers.image.source="https://github.com/your-org/ai-pr-reviewer"

# Create non-root user
RUN groupadd -r aipr && useradd -r -g aipr aipr

WORKDIR /app

# Copy engine library
COPY --from=engine-builder /build/build/lib* /app/lib/

# Copy server JAR
COPY --from=server-builder /build/server/build/libs/*.jar /app/server.jar

# Copy config
COPY config/ /app/config/
COPY schemas/ /app/schemas/
COPY core/prompting/ /app/prompting/

# Set library path
ENV LD_LIBRARY_PATH=/app/lib

# Create data directories
RUN mkdir -p /app/data /app/logs && chown -R aipr:aipr /app

USER aipr

# Expose ports
EXPOSE 8080 50051

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8080/actuator/health || exit 1

# Default environment
ENV JAVA_OPTS="-Xms512m -Xmx2g -XX:+UseG1GC"
ENV SPRING_PROFILES_ACTIVE=production

# Entry point
ENTRYPOINT ["sh", "-c", "java $JAVA_OPTS -jar /app/server.jar"]
