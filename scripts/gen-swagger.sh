#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="$ROOT_DIR/docs/openapi"

if ! command -v swag >/dev/null 2>&1; then
  echo "swag not found. Install with:"
  echo "  go install github.com/swaggo/swag/cmd/swag@latest"
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

cd "$ROOT_DIR"
swag init   --generalInfo cmd/server/main.go   --output "$OUTPUT_DIR"   --parseDependency   --parseInternal   --generatedTime=false

echo "Agent Ecommerce OpenAPI generated at: $OUTPUT_DIR"
