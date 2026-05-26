#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# shellcheck source=tools/prod/lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

DRY_RUN=0
FAIL_ON_CRITICAL=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env) shift; [ "${1:-}" = "prod" ] || fail "unsupported_env env=${1:-}" ;;
    --dry-run) DRY_RUN=1 ;;
    --fail-on-critical) FAIL_ON_CRITICAL=1 ;;
    --help|-h)
      cat <<'USAGE'
Usage: tools/prod/ecommerce-drift-check.sh [--env prod] [--dry-run] [--fail-on-critical]
Read-only prod topology/config/DB/Platform-contract drift check. Prints no secrets.
USAGE
      exit 0
      ;;
    *) fail "unknown_arg arg=$1" ;;
  esac
  shift || true
done

load_topology
if [ "$DRY_RUN" = "1" ]; then
  log "DRY_RUN ecommerce-drift-check env=${ENV_NAME:-prod} remote=$REMOTE remote_dir=$REMOTE_DIR"
  log "Would check: prod container/health/ready, Ecommerce config, Platform URL/secret hash, product/runtime DB tables, and Platform endpoint contract."
  exit 0
fi

set +e
remote_cmd "ECOMMERCE_CONTAINER=$(printf '%q' "${ECOMMERCE_CONTAINER:-v-ecommerce-backend}") PLATFORM_CONTAINER=$(printf '%q' "${PLATFORM_CONTAINER:-v-platform-backend}") ECOMMERCE_CONFIG_PATH=$(printf '%q' "${ECOMMERCE_CONFIG_PATH:-$REMOTE_DIR/config.prod.yaml}") PLATFORM_CONFIG_PATH=$(printf '%q' "${PLATFORM_CONFIG_PATH:-${PLATFORM_REMOTE_DIR:-/root/gk/platform-backend}/config.prod.yaml}") ECOMMERCE_LOCAL_URL=$(printf '%q' "${ECOMMERCE_LOCAL_URL:-http://127.0.0.1:${ECOMMERCE_HOST_PORT:-8296}}") ECOMMERCE_INTERNAL_URL=$(printf '%q' "${ECOMMERCE_INTERNAL_URL:-http://v-ecommerce-backend:8296}") PLATFORM_INTERNAL_URL=$(printf '%q' "${PLATFORM_INTERNAL_URL:-http://v-platform-backend:8095}") DEV_ECOMMERCE_HOST_PORT=$(printf '%q' "${DEV_ECOMMERCE_HOST_PORT:-8396}") DEV_PLATFORM_HOST_PORT=$(printf '%q' "${DEV_PLATFORM_HOST_PORT:-8195}") DEV_ECOMMERCE_CONTAINER=$(printf '%q' "${DEV_ECOMMERCE_CONTAINER:-v-ecommerce-backend-dev}") DEV_PLATFORM_CONTAINER=$(printf '%q' "${DEV_PLATFORM_CONTAINER:-v-platform-backend-dev}") DB_CONTAINER=$(printf '%q' "${DB_CONTAINER:-kong-database}") DB_USER=$(printf '%q' "${DB_USER:-kong}") DB_NAME=$(printf '%q' "${DB_NAME:-kong}") ECOMMERCE_REQUIRED_TABLES=$(printf '%q' "${ECOMMERCE_REQUIRED_TABLES:-ecom_product_sku ecommerce_assets ecommerce_image_jobs ecommerce_visual_workflow_sessions ecommerce_visual_source_references ecommerce_visual_deconstruction_jobs ecommerce_visual_deconstruction_elements}") bash -s" <<'REMOTE' | redact_stream
set -euo pipefail
critical=0
warns=0
pass() { echo "PASS $*"; }
warn() { echo "WARN $*"; warns=$((warns+1)); }
crit() { echo "CRITICAL $*"; critical=$((critical+1)); }

command -v python3 >/dev/null 2>&1 || { echo "CRITICAL missing_remote_command command=python3"; exit 1; }
ecom_cfg="${ECOMMERCE_CONFIG_PATH:-/root/gk/ecommerce-backend/config.prod.yaml}"
platform_cfg="${PLATFORM_CONFIG_PATH:-/root/gk/platform-backend/config.prod.yaml}"
[ -f "$ecom_cfg" ] || crit "ecommerce_config_missing path=$ecom_cfg"
[ -f "$platform_cfg" ] || crit "platform_config_missing path=$platform_cfg"

for name in "${ECOMMERCE_CONTAINER:-v-ecommerce-backend}" "${PLATFORM_CONTAINER:-v-platform-backend}"; do
  if docker inspect "$name" >/dev/null 2>&1; then
    image=$(docker inspect -f '{{.Config.Image}}' "$name" 2>/dev/null || true)
    health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name" 2>/dev/null || true)
    if [ "$health" = healthy ]; then pass "container name=$name image=$image health=$health"; else crit "container_unhealthy name=$name image=$image health=$health"; fi
  else
    crit "container_missing name=$name"
  fi
done

base="${ECOMMERCE_LOCAL_URL:-http://127.0.0.1:8296}"
for endpoint in healthz readyz; do
  code=$(curl -fsS -o /tmp/ecommerce_${endpoint}.json -w '%{http_code}' "$base/$endpoint" 2>/tmp/ecommerce_${endpoint}.err || true)
  body_code=$(python3 - <<PY 2>/dev/null || true
import json
try:
    print(json.load(open('/tmp/ecommerce_${endpoint}.json')).get('code',''))
except Exception:
    print('')
PY
)
  if [ "$code" = "200" ] && { [ "$body_code" = "0" ] || [ -z "$body_code" ]; }; then
    pass "http endpoint=/$endpoint status=$code code=${body_code:-n/a}"
  else
    crit "http endpoint=/$endpoint status=${code:-none} code=${body_code:-n/a}"
  fi
done
rm -f /tmp/ecommerce_healthz.json /tmp/ecommerce_readyz.json /tmp/ecommerce_healthz.err /tmp/ecommerce_readyz.err

python3 - <<'PY' > /tmp/ecommerce_drift_env.sh
import yaml, os, re, shlex, hashlib

def safe_load(path):
    try:
        with open(path) as f:
            return yaml.safe_load(f) or {}
    except Exception:
        return {}

ecfg=safe_load(os.environ.get('ECOMMERCE_CONFIG_PATH','/root/gk/ecommerce-backend/config.prod.yaml'))
pcfg=safe_load(os.environ.get('PLATFORM_CONFIG_PATH','/root/gk/platform-backend/config.prod.yaml'))
ph=lambda s: hashlib.sha256((s or '').encode()).hexdigest()[:12] if s else ''
placeholder=lambda s: bool(re.search(r'(change-me|changeme|placeholder|example-secret|your-secret)', s or '', re.I))
ecom_sec=(ecfg.get('security') or {}).get('service_secret_key') or ''
ecom_platform=(ecfg.get('platform') or {}).get('internal_service_secret') or ''
ecom_platform_url=(ecfg.get('platform') or {}).get('base_url') or ''
platform_sec=(pcfg.get('security') or {}).get('internal_service_secret') or ''
print('ECOM_CALLBACK_EMPTY=%s' % (not bool(ecom_sec)))
print('ECOM_CALLBACK_PLACEHOLDER=%s' % placeholder(ecom_sec))
print('ECOM_CALLBACK_HASH=%s' % ph(ecom_sec))
print('ECOM_PLATFORM_URL=%s' % shlex.quote(ecom_platform_url))
ecom_platform_jwt=(ecfg.get('platform') or {}).get('jwt_secret') or ''
platform_jwt=(pcfg.get('security') or {}).get('jwt_secret') or ''
print('ECOM_PLATFORM_SECRET_HASH=%s' % ph(ecom_platform))
print('PLATFORM_INTERNAL_HASH=%s' % ph(platform_sec))
print('ECOM_PLATFORM_JWT_HASH=%s' % ph(ecom_platform_jwt))
print('PLATFORM_JWT_HASH=%s' % ph(platform_jwt))
print('ECOM_PLATFORM_JWT_EMPTY=%s' % (not bool(ecom_platform_jwt)))
print('PLATFORM_JWT_EMPTY=%s' % (not bool(platform_jwt)))
print('ECOM_PLATFORM_JWT_PLACEHOLDER=%s' % placeholder(ecom_platform_jwt))
print('ECOM_DB_DRIVER=%s' % shlex.quote(str((ecfg.get('database') or {}).get('driver') or '')))
print('ECOM_AUTO_MIGRATE=%s' % shlex.quote(str((ecfg.get('database') or {}).get('auto_migrate_enabled'))))
PY
# shellcheck disable=SC1091
source /tmp/ecommerce_drift_env.sh
[ "${ECOM_CALLBACK_EMPTY:-True}" = "False" ] && pass "ecommerce callback_secret_non_empty=true" || crit "ecommerce callback_secret_empty=true"
[ "${ECOM_CALLBACK_PLACEHOLDER:-True}" = "False" ] && pass "ecommerce callback_secret_placeholder=false" || crit "ecommerce callback_secret_placeholder=true"
[ "${ECOM_PLATFORM_SECRET_HASH:-}" = "${PLATFORM_INTERNAL_HASH:-}" ] && pass "ecommerce outbound_platform_secret_matches_platform=true hash_prefix=${ECOM_PLATFORM_SECRET_HASH:-}" || crit "ecommerce outbound_platform_secret_matches_platform=false ecommerce_hash=${ECOM_PLATFORM_SECRET_HASH:-} platform_hash=${PLATFORM_INTERNAL_HASH:-}"
[ "${ECOM_PLATFORM_JWT_EMPTY:-True}" = "False" ] && pass "ecommerce platform_jwt_secret_configured=true" || crit "ecommerce platform_jwt_secret_missing=true follow_up=configure_and_rotate_platform_jwt_secret"
[ "${PLATFORM_JWT_EMPTY:-True}" = "False" ] && pass "platform jwt_secret_configured=true" || crit "platform jwt_secret_missing=true"
[ "${ECOM_PLATFORM_JWT_PLACEHOLDER:-False}" = "False" ] && pass "ecommerce platform_jwt_secret_placeholder=false" || crit "ecommerce platform_jwt_secret_placeholder=true"
[ "${ECOM_PLATFORM_JWT_HASH:-}" = "${PLATFORM_JWT_HASH:-}" ] && pass "ecommerce platform_jwt_secret_matches_platform=true hash_prefix=${ECOM_PLATFORM_JWT_HASH:-}" || crit "ecommerce platform_jwt_secret_matches_platform=false ecommerce_hash=${ECOM_PLATFORM_JWT_HASH:-} platform_hash=${PLATFORM_JWT_HASH:-}"
[ "${ECOM_AUTO_MIGRATE:-}" = "False" ] && pass "config auto_migrate_enabled=false" || warn "config auto_migrate_enabled=${ECOM_AUTO_MIGRATE:-unknown} expected=False"
if [ "${ECOM_PLATFORM_URL:-}" = "${PLATFORM_INTERNAL_URL:-http://v-platform-backend:8095}" ]; then
  pass "ecommerce platform_base_url=${ECOM_PLATFORM_URL}"
elif printf '%s' "${ECOM_PLATFORM_URL:-}" | grep -Eq -- "${DEV_PLATFORM_CONTAINER:-v-platform-backend-dev}|:${DEV_PLATFORM_HOST_PORT:-8195}(/|$)|localhost:${DEV_PLATFORM_HOST_PORT:-8195}|127.0.0.1:${DEV_PLATFORM_HOST_PORT:-8195}"; then
  crit "ecommerce platform_base_url_points_to_dev url=${ECOM_PLATFORM_URL:-empty}"
else
  crit "ecommerce platform_base_url_mismatch actual=${ECOM_PLATFORM_URL:-empty} expected=${PLATFORM_INTERNAL_URL:-http://v-platform-backend:8095}"
fi

DB_CONTAINER="${DB_CONTAINER:-kong-database}"
DB_USER="${DB_USER:-kong}"
DB_NAME="${DB_NAME:-kong}"
if ! docker exec "$DB_CONTAINER" sh -lc 'command -v psql >/dev/null 2>&1'; then
  crit "db_psql_missing container=$DB_CONTAINER"
  rows=""
else
  rows=$(docker exec -i "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -At <<SQL
select 'table|' || t || '|' || (to_regclass(t) is not null)::text from unnest(string_to_array('${ECOMMERCE_REQUIRED_TABLES:-}', ' ')) as t;
select 'endpoint|' || product_code || '|' || base_url || '|' || status || '|' || (length(coalesce(secret,''))=0)::text || '|' || (left(coalesce(secret,''),9)='change-me')::text || '|' || left(encode(sha256(coalesce(secret,'')::bytea),'hex'),12) from runtime_product_endpoints where product_code='ecommerce';
SQL
)
fi
while IFS='|' read -r kind a b c d e f; do
  [ -n "${kind:-}" ] || continue
  case "$kind" in
    table)
      [ "$b" = "true" ] && pass "db_table name=$a exists=true" || crit "db_table_missing name=$a"
      ;;
    endpoint)
      base_url=$b; status=$c; secret_empty=$d; secret_placeholder=$e; hash=$f
      if [ "$base_url" = "${ECOMMERCE_INTERNAL_URL:-http://v-ecommerce-backend:8296}" ]; then
        pass "platform_endpoint product=ecommerce base_url=$base_url"
      elif printf '%s' "$base_url" | grep -Eq -- "${DEV_ECOMMERCE_CONTAINER:-v-ecommerce-backend-dev}|:${DEV_ECOMMERCE_HOST_PORT:-8396}(/|$)|localhost:${DEV_ECOMMERCE_HOST_PORT:-8396}|127.0.0.1:${DEV_ECOMMERCE_HOST_PORT:-8396}"; then
        crit "platform_endpoint product=ecommerce points_to_dev base_url=$base_url"
      else
        crit "platform_endpoint product=ecommerce base_url_mismatch actual=$base_url expected=${ECOMMERCE_INTERNAL_URL:-http://v-ecommerce-backend:8296}"
      fi
      [ "$status" = "active" ] && pass "platform_endpoint product=ecommerce active" || crit "platform_endpoint product=ecommerce status=$status"
      [ "$secret_empty" = "false" ] && pass "platform_endpoint product=ecommerce secret_non_empty=true" || crit "platform_endpoint product=ecommerce secret_empty=true"
      [ "$secret_placeholder" = "false" ] && pass "platform_endpoint product=ecommerce secret_placeholder=false" || crit "platform_endpoint product=ecommerce secret_placeholder=true"
      [ "$hash" = "${ECOM_CALLBACK_HASH:-}" ] && pass "platform_endpoint product=ecommerce callback_secret_hash_matches_backend=true hash_prefix=$hash" || crit "platform_endpoint product=ecommerce callback_secret_hash_matches_backend=false endpoint_hash=$hash backend_hash=${ECOM_CALLBACK_HASH:-}"
      ;;
  esac
done <<< "$rows"
if ! printf '%s\n' "$rows" | grep -q '^endpoint|ecommerce|'; then crit "platform_endpoint product=ecommerce missing=true"; fi
rm -f /tmp/ecommerce_drift_env.sh

echo "SUMMARY critical=$critical warnings=$warns"
exit "$critical"
REMOTE
status=${PIPESTATUS[0]}
set -e
if [ "$status" -ne 0 ] && [ "$FAIL_ON_CRITICAL" = "1" ]; then
  exit "$status"
fi
exit 0
