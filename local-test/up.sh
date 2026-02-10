#!/usr/bin/env bash
# Start local MinIO (keep running for manual testing)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

docker compose -f "$SCRIPT_DIR/docker-compose.yml" up -d --wait

echo "MinIO UI: http://localhost:9001 (minioadmin/minioadmin)"
echo "S3 endpoint: http://localhost:9000"
