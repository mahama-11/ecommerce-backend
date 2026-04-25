#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

git -C "$ROOT_DIR" config core.hooksPath .githooks
chmod +x "$ROOT_DIR/.githooks/pre-commit"
chmod +x "$ROOT_DIR/scripts/pre-commit.sh"
chmod +x "$ROOT_DIR/scripts/test-quick.sh"
chmod +x "$ROOT_DIR/scripts/test-all.sh"
chmod +x "$ROOT_DIR/scripts/check-guardrails.sh"

echo "✅ Installed git hooks for v-ecommerce-backend"
