#!/usr/bin/env bash
# Run integration tests against local MinIO (starts and tears down automatically)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Starting MinIO..."
docker compose -f "$SCRIPT_DIR/docker-compose.yml" up -d --wait

echo "Running integration tests..."
set +e
go test -v -tags=integration -count=1 "$REPO_ROOT/test/integration/..."
EXIT_CODE=$?
set -e

echo "Stopping MinIO..."
docker compose -f "$SCRIPT_DIR/docker-compose.yml" down

exit $EXIT_CODE
