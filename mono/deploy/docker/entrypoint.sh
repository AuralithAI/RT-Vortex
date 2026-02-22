#!/bin/bash
#
# AI PR Reviewer - Docker Entrypoint
#
# Starts both the C++ Engine (gRPC server) and Java Server.
# The engine must be running before the Java server starts.
#

set -e

# Configuration
ENGINE_HOST="${ENGINE_HOST:-0.0.0.0}"
ENGINE_PORT="${ENGINE_PORT:-50051}"
ENGINE_CONFIG="${ENGINE_CONFIG:-/app/config/default.yml}"

JAVA_OPTS="${JAVA_OPTS:--Xms512m -Xmx2g -XX:+UseG1GC}"

echo "=============================================="
echo " AI PR Reviewer - Starting Services"
echo "=============================================="
echo ""

# Function to handle shutdown
cleanup() {
    echo ""
    echo "[INFO] Shutting down..."
    
    # Kill Java server
    if [ -n "$JAVA_PID" ]; then
        echo "[INFO] Stopping Java server (PID: $JAVA_PID)..."
        kill -TERM "$JAVA_PID" 2>/dev/null || true
        wait "$JAVA_PID" 2>/dev/null || true
    fi
    
    # Kill engine
    if [ -n "$ENGINE_PID" ]; then
        echo "[INFO] Stopping Engine (PID: $ENGINE_PID)..."
        kill -TERM "$ENGINE_PID" 2>/dev/null || true
        wait "$ENGINE_PID" 2>/dev/null || true
    fi
    
    echo "[INFO] Shutdown complete."
    exit 0
}

# Trap signals
trap cleanup SIGTERM SIGINT SIGQUIT

# Start C++ Engine
echo "[INFO] Starting C++ Engine..."
echo "       Host: $ENGINE_HOST"
echo "       Port: $ENGINE_PORT"
echo "       Config: $ENGINE_CONFIG"

/app/bin/aipr-engine \
    --host "$ENGINE_HOST" \
    --port "$ENGINE_PORT" \
    --config "$ENGINE_CONFIG" \
    &

ENGINE_PID=$!
echo "[INFO] Engine started (PID: $ENGINE_PID)"

# Wait for engine to be ready
echo "[INFO] Waiting for engine to be ready..."
RETRIES=30
for i in $(seq 1 $RETRIES); do
    if nc -z localhost "$ENGINE_PORT" 2>/dev/null; then
        echo "[INFO] Engine is ready on port $ENGINE_PORT"
        break
    fi
    
    if [ $i -eq $RETRIES ]; then
        echo "[ERROR] Engine failed to start within timeout"
        exit 1
    fi
    
    sleep 1
done

# Build classpath
CLASSPATH=""
for jar in /app/lib/*.jar; do
    if [ -z "$CLASSPATH" ]; then
        CLASSPATH="$jar"
    else
        CLASSPATH="$CLASSPATH:$jar"
    fi
done

# Start Java Server
echo ""
echo "[INFO] Starting Java Server..."
echo "       JAVA_OPTS: $JAVA_OPTS"
echo "       Engine: localhost:$ENGINE_PORT"

java $JAVA_OPTS \
    -Drt.home=/app \
    -cp "$CLASSPATH" \
    ai.aipr.server.AiprServerApplication \
    &

JAVA_PID=$!
echo "[INFO] Java server started (PID: $JAVA_PID)"

echo ""
echo "=============================================="
echo " Services Running"
echo "=============================================="
echo " Engine:  localhost:$ENGINE_PORT (gRPC)"
echo " Server:  localhost:8080 (REST)"
echo "          localhost:9090 (gRPC)"
echo "=============================================="
echo ""

# Wait for either process to exit
wait -n

# If we get here, one process died - clean up the other
cleanup
