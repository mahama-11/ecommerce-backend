#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

echo "======================================"
echo "⚡ Ecommerce Backend Pre-commit"
echo "======================================"

echo "[1/3] Running guardrails..."
bash "$ROOT_DIR/scripts/check-guardrails.sh"
echo "✅ Guardrails passed."

echo "[2/3] Running gofmt on changed files..."
STAGED_GO_FILES=$(git -C "$ROOT_DIR" diff --cached --name-only --diff-filter=d | grep -E '.*\.go$' || true)
if [[ -n "$STAGED_GO_FILES" ]]; then
  while IFS= read -r FILE; do
    [[ -z "$FILE" ]] && continue
    gofmt -w "$ROOT_DIR/$FILE"
    git -C "$ROOT_DIR" add "$FILE"
  done <<< "$STAGED_GO_FILES"
fi
echo "✅ gofmt passed."

echo "[3/3] Running quick Go tests..."
bash "$ROOT_DIR/scripts/test-quick.sh"
echo "✅ Ecommerce backend pre-commit passed!"
