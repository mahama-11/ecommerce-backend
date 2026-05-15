#!/usr/bin/env bash
set -euo pipefail

script_dir() {
  CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
}

TOOLS_PROD_DIR="$(CDPATH= cd -- "$(script_dir)/.." && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$TOOLS_PROD_DIR/../.." && pwd)"
TOPOLOGY_FILE_DEFAULT="$REPO_ROOT/ops/topology/prod.env"

load_topology() {
  local file="${TOPOLOGY_FILE:-$TOPOLOGY_FILE_DEFAULT}"
  if [ ! -f "$file" ]; then
    echo "FAIL topology_missing path=$file" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  source "$file"
  REMOTE="${REMOTE:-${REMOTE_DEFAULT:-root@159.138.228.40}}"
  SSH_KEY="${SSH_KEY:-${SSH_KEY_DEFAULT:-$HOME/.ssh/KeyPair-v2.pem}}"
  REMOTE_DIR="${REMOTE_DIR:-${REMOTE_DIR_DEFAULT:-/root/gk/ecommerce-backend}}"
  REMOTE_BASE="${REMOTE_BASE:-${REMOTE_BASE_DEFAULT:-${REMOTE_DIR%/*}}}"
  ECOMMERCE_IMAGE="${IMAGE_NAME:-${ECOMMERCE_IMAGE:-ver/v-ecommerce-backend}}"
  validate_topology
}

log() { printf '%s\n' "$*"; }
warn() { printf 'WARN %s\n' "$*"; }
fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }

remote_cmd() {
  ssh -i "$SSH_KEY" "$REMOTE" "$@"
}

remote_bash() {
  ssh -i "$SSH_KEY" "$REMOTE" 'bash -s'
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || fail "missing_command command=$cmd"
}

validate_var() {
  local name="$1" value="$2" regex="$3"
  if [ -n "$value" ] && ! printf '%s' "$value" | grep -Eq -- "$regex"; then
    fail "unsafe_topology_value name=$name"
  fi
}

validate_topology() {
  validate_var REMOTE "$REMOTE" '^[A-Za-z0-9_.@:-]+$'
  validate_var SSH_KEY "$SSH_KEY" '^/[A-Za-z0-9_./@+=:-]+$'
  validate_var REMOTE_DIR "$REMOTE_DIR" '^/[A-Za-z0-9_./+=:-]+$'
  validate_var REMOTE_BASE "$REMOTE_BASE" '^/[A-Za-z0-9_./+=:-]+$'
  validate_var ECOMMERCE_IMAGE "$ECOMMERCE_IMAGE" '^[A-Za-z0-9_./:-]+$'
  for name in ECOMMERCE_CONTAINER PLATFORM_CONTAINER DEV_ECOMMERCE_CONTAINER DEV_PLATFORM_CONTAINER DB_CONTAINER DB_USER DB_NAME ECOMMERCE_SERVICE PLATFORM_RUNTIME_QUEUE_NAME; do
    validate_var "$name" "${!name:-}" '^[A-Za-z0-9_.:-]+$'
  done
  for name in ECOMMERCE_LOCAL_URL ECOMMERCE_INTERNAL_URL ECOMMERCE_HEALTH_URL ECOMMERCE_READY_URL PLATFORM_LOCAL_URL PLATFORM_INTERNAL_URL; do
    validate_var "$name" "${!name:-}" '^https?://[A-Za-z0-9_.:-]+(/[A-Za-z0-9_./?=&%:-]*)?$'
  done
  for name in ECOMMERCE_CONFIG_PATH PLATFORM_CONFIG_PATH PLATFORM_REMOTE_DIR; do
    validate_var "$name" "${!name:-}" '^/[A-Za-z0-9_./+=:-]+$'
  done
  validate_var ECOMMERCE_REQUIRED_TABLES "${ECOMMERCE_REQUIRED_TABLES:-}" '^[A-Za-z0-9_ ]+$'
  validate_var EXPECTED_TEXT_TASKS "${EXPECTED_TEXT_TASKS:-}" '^[A-Za-z0-9_ ]+$'
}

redact_stream() {
  sed -E \
    -e 's/(api[_-]?key[[:space:]]*[:=][[:space:]]*)[^,}[:space:]]+/\1[REDACTED]/Ig' \
    -e 's/(authorization[[:space:]]*[:=][[:space:]]*)[^,}[:space:]]+/\1[REDACTED]/Ig' \
    -e 's/(jwt[._-]?secret[[:space:]]*[:=][[:space:]]*)[^,}[:space:]]+/\1[REDACTED]/Ig' \
    -e 's/(secret[[:space:]]*[:=][[:space:]]*)[^,}[:space:]]+/\1[REDACTED]/Ig' \
    -e 's/(password[[:space:]]*[:=][[:space:]]*)[^,}[:space:]]+/\1[REDACTED]/Ig' \
    -e 's/(token[[:space:]]*[:=][[:space:]]*)[^,}[:space:]]+/\1[REDACTED]/Ig' \
    -e 's/(Bearer[[:space:]]+)[A-Za-z0-9._~-]+/\1[REDACTED]/g'
}

write_evidence_header() {
  local file="$1" title="$2"
  mkdir -p "$(dirname "$file")"
  {
    echo "# $title"
    echo
    echo "- generated_at: $(date -Iseconds)"
    echo "- git_sha: $(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
    echo
  } > "$file"
}
