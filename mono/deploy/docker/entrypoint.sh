#!/bin/sh
#
# RTVortex Server — Docker Entrypoint
#
# Starts the Go API server. The C++ engine runs as a separate container
# and is configured via ENGINE_HOST / ENGINE_PORT environment variables.
#

set -e

echo "=============================================="
echo " RTVortex Server — Starting"
echo "=============================================="
echo ""
echo " Engine:     ${ENGINE_HOST:-localhost}:${ENGINE_PORT:-50051}"
echo " HTTP:       0.0.0.0:${SERVER_PORT:-8080}"
echo " Database:   ${DATABASE_HOST:-localhost}:${DATABASE_PORT:-5432}/${DATABASE_NAME:-rtvortex}"
echo " Redis:      ${REDIS_HOST:-localhost}:${REDIS_PORT:-6379}"
echo " Log Level:  ${LOG_LEVEL:-info}"
echo ""
echo "=============================================="
echo ""

exec /app/RTVortexGo "$@"
