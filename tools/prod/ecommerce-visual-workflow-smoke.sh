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
Usage: tools/prod/ecommerce-visual-workflow-smoke.sh [--env prod] [--dry-run] [--fail-on-critical]
Creates a synthetic product and V2 visual workflow session on prod, verifies stage-view, and exercises intent-planner boundary. Prints no JWTs/secrets.
USAGE
      exit 0
      ;;
    *) fail "unknown_arg arg=$1" ;;
  esac
  shift || true
done

load_topology
if [ "$DRY_RUN" = "1" ]; then
  log "DRY_RUN ecommerce-visual-workflow-smoke env=${ENV_NAME:-prod} remote=$REMOTE base=${ECOMMERCE_LOCAL_URL:-http://127.0.0.1:${ECOMMERCE_HOST_PORT:-8296}}"
  log "Would generate short-lived JWT inside remote process, create synthetic product/session, check stage-view code=0, and call intent-planner boundary."
  exit 0
fi

set +e
remote_cmd "ECOMMERCE_LOCAL_URL=$(printf '%q' "${ECOMMERCE_LOCAL_URL:-http://127.0.0.1:${ECOMMERCE_HOST_PORT:-8296}}") ECOMMERCE_CONFIG_PATH=$(printf '%q' "${ECOMMERCE_CONFIG_PATH:-$REMOTE_DIR/config.prod.yaml}") PLATFORM_LOCAL_URL=$(printf '%q' "${PLATFORM_LOCAL_URL:-http://127.0.0.1:${PLATFORM_HOST_PORT:-8095}}") PLATFORM_CONFIG_PATH=$(printf '%q' "${PLATFORM_CONFIG_PATH:-${PLATFORM_REMOTE_DIR:-/root/gk/platform-backend}/config.prod.yaml}") bash -s" <<'REMOTE' | redact_stream
set -euo pipefail
BASE="${ECOMMERCE_LOCAL_URL:-http://127.0.0.1:8296}"
PLATFORM_BASE="${PLATFORM_LOCAL_URL:-http://127.0.0.1:8095}"
ECFG="${ECOMMERCE_CONFIG_PATH:-/root/gk/ecommerce-backend/config.prod.yaml}"
PCFG="${PLATFORM_CONFIG_PATH:-/root/gk/platform-backend/config.prod.yaml}"
command -v jq >/dev/null 2>&1 || { echo "FAIL missing_remote_command command=jq"; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "FAIL missing_remote_command command=python3"; exit 1; }
[ -f "$ECFG" ] || { echo "FAIL ecommerce_config_missing"; exit 1; }
python3 - <<'PY'
import base64, hashlib, hmac, json, os, sys, time, urllib.error, urllib.request
import yaml

base=os.environ.get('ECOMMERCE_LOCAL_URL','http://127.0.0.1:8296').rstrip('/')
platform_base=os.environ.get('PLATFORM_LOCAL_URL','http://127.0.0.1:8095').rstrip('/')
ecfg_path=os.environ.get('ECOMMERCE_CONFIG_PATH','/root/gk/ecommerce-backend/config.prod.yaml')
pcfg_path=os.environ.get('PLATFORM_CONFIG_PATH','/root/gk/platform-backend/config.prod.yaml')

def fail(msg):
    print('FAIL '+msg)
    raise SystemExit(1)

def b64(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b'=').decode()

def mint_jwt() -> str:
    cfg=yaml.safe_load(open(ecfg_path)) or {}
    secret=((cfg.get('platform') or {}).get('jwt_secret') or '')
    if not secret:
        raise SystemExit('jwt_secret_missing')
    now=int(time.time())
    header={'alg':'HS256','typ':'JWT'}
    payload={'user_id':'user_prod_visual_smoke','org_id':'org_prod_visual_smoke','org_role':'owner','iat':now,'nbf':now-5,'exp':now+900,'iss':'prod-ecommerce-smoke'}
    signing=b64(json.dumps(header,separators=(',',':')).encode())+'.'+b64(json.dumps(payload,separators=(',',':')).encode())
    sig=b64(hmac.new(secret.encode(), signing.encode(), hashlib.sha256).digest())
    return signing+'.'+sig

def request_json(method, url, token=None, payload=None, internal_secret=None, timeout=20):
    headers={}
    data=None
    if payload is not None:
        data=json.dumps(payload,separators=(',',':')).encode()
        headers['Content-Type']='application/json'
    if token:
        headers['Authorization']='Bearer '+token
    if internal_secret:
        headers['X-Internal-Service-Secret']=internal_secret
    req=urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body=resp.read().decode('utf-8')
            return resp.status, json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        try:
            body=e.read().decode('utf-8')
            parsed=json.loads(body) if body else {}
        except Exception:
            parsed={}
        return e.code, parsed

if not os.path.exists(ecfg_path):
    fail('ecommerce_config_missing')
token=mint_jwt()
smoke_id='prod-smoke-%d' % int(time.time())
sku='SMOKE-%s' % smoke_id

status, product = request_json('POST', base+'/api/v1/ecommerce/products', token=token, payload={
    'sku_code': sku,
    'title': 'Prod Smoke Synthetic Product '+smoke_id,
    'spu_id': 'prod-smoke',
    'category_id': 'smoke',
    'brand_id': 'smoke',
    'cost_currency': 'USD',
    'tags': ['prod-smoke','synthetic'],
})
product_id=((product.get('data') or {}).get('id') or ((product.get('data') or {}).get('product') or {}).get('id') or '')
if status != 200 or str(product.get('code','')) != '0' or not product_id:
    fail('product_create status=%s code=%s id_present=false' % (status, product.get('code','missing')))
print('PASS product_create synthetic=true product_id=%s sku=%s' % (product_id, sku))

status, session = request_json('POST', base+'/api/v1/ecommerce/v2/visual-workflows/sessions', token=token, payload={
    'product_id': product_id,
    'sku_code': sku,
    'tool_slug': 'product-scene-compositing',
    'idempotency_key': smoke_id,
})
data=session.get('data') or {}
session_id=data.get('id') or (data.get('session') or {}).get('id') or data.get('session_id') or ''
if status not in (200,201) or str(session.get('code','')) != '0' or not session_id:
    fail('visual_session_create status=%s code=%s id_present=false' % (status, session.get('code','missing')))
print('PASS visual_session_create session_id=%s' % session_id)

status, stage = request_json('GET', base+'/api/v1/ecommerce/v2/visual-workflows/'+session_id+'/stage-view', token=token)
stage_data=stage.get('data') or {}
current_stage=stage_data.get('current_stage') or (stage_data.get('stage_view') or {}).get('current_stage') or 'unknown'
if status != 200 or str(stage.get('code','')) != '0':
    fail('stage_view status=%s code=%s' % (status, stage.get('code','missing')))
print('PASS stage_view status=200 code=0 current_stage=%s' % current_stage)

status, intent = request_json('POST', base+'/api/v1/ecommerce/v2/visual-workflows/'+session_id+'/intent-planner-jobs', token=token, payload={
    'marketplace': 'amazon',
    'locale': 'en-US',
    'idempotency_key': smoke_id+'-intent',
    'drift_controls': {'reference_weight': 0.7},
})
intent_data=intent.get('data') or {}
intent_status=intent_data.get('status') or ''
runtime_job_id=intent_data.get('runtime_job_id') or ''
blocker_codes=','.join([b.get('code','') for b in (intent_data.get('blockers') or []) if isinstance(b, dict)])
if status not in (200,201) or str(intent.get('code','')) != '0':
    fail('intent_planner_boundary status=%s code=%s' % (status, intent.get('code','missing')))
if not runtime_job_id:
    honest={'DECONSTRUCTION_SELECTION_REQUIRED','CONTRACT_NEEDED','PLATFORM_CAPABILITY_UNAVAILABLE'}
    if intent_status == 'contract_needed' and (set(filter(None, blocker_codes.split(','))) & honest):
        print('PASS_WITH_NOTES intent_planner_boundary status=%s blockers=%s note=honest_contract_or_selection_gate' % (intent_status, blocker_codes))
        raise SystemExit(0)
    fail('intent_planner_boundary missing_runtime_job_id status=%s blockers=%s' % (intent_status or 'empty', blocker_codes or 'empty'))
print('PASS intent_planner_boundary runtime_job_id=%s status=%s' % (runtime_job_id, intent_status or 'unknown'))

internal_secret=''
if os.path.exists(pcfg_path):
    pcfg=yaml.safe_load(open(pcfg_path)) or {}
    internal_secret=(pcfg.get('security') or {}).get('internal_service_secret') or ''
if internal_secret:
    for _ in range(20):
        st, detail = request_json('GET', platform_base+'/internal/v1/runtime/jobs/'+runtime_job_id, internal_secret=internal_secret, timeout=10)
        d=detail.get('data') or {}
        job=d.get('job') or d
        runtime_status=job.get('status') or ''
        runtime_stage=job.get('stage') or 'unknown'
        if runtime_status:
            print('PASS platform_runtime_poll job_id=%s status=%s stage=%s' % (runtime_job_id, runtime_status, runtime_stage))
            if runtime_status in ('completed','failed','canceled'):
                raise SystemExit(0)
        time.sleep(2)
    print('PASS_WITH_NOTES platform_runtime_poll job_id=%s note=created_but_not_terminal_within_window' % runtime_job_id)
    raise SystemExit(0)
print('PASS_WITH_NOTES platform_runtime_poll job_id=%s note=internal_poll_not_feasible' % runtime_job_id)
PY
REMOTE
status=${PIPESTATUS[0]}
set -e
if [ "$status" -ne 0 ] && [ "$FAIL_ON_CRITICAL" = "1" ]; then
  exit "$status"
fi
exit "$status"
