#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "[guardrails] checking direct gorm.Open usage outside internal/storage"
if grep -R "gorm.Open(" internal --include='*.go' | grep -v 'internal/storage/storage.go' | grep -v '_test.go' >/dev/null; then
  echo "guardrail failed: gorm.Open should only exist in internal/storage/storage.go"
  exit 1
fi

echo "[guardrails] ok"
