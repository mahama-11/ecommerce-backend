#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# shellcheck source=tools/prod/lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

PHASE="${1:-all}"
if [ "$#" -gt 0 ]; then shift; fi
DRY_RUN="${DRY_RUN:-0}"
SKIP_SMOKE="${SKIP_SMOKE:-0}"
UPLOAD_PROD_CONFIG="${UPLOAD_PROD_CONFIG:-0}"
PROD_TAG_IN="${PROD_TAG:-}"
IMAGE_TAR_FILE="${IMAGE_TAR_FILE:-${IMAGE_TAR:-}}"
DEFAULT_GOPROXY="${DEPLOY_GOPROXY:-https://goproxy.cn,direct}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env) shift; [ "${1:-}" = "prod" ] || fail "unsupported_env env=${1:-}" ;;
    --dry-run) DRY_RUN=1 ;;
    --tag) shift; PROD_TAG_IN="${1:-}"; [ -n "$PROD_TAG_IN" ] || fail "missing_tag" ;;
    --skip-smoke) SKIP_SMOKE=1 ;;
    --upload-prod-config) UPLOAD_PROD_CONFIG=1 ;;
    --help|-h)
      cat <<'USAGE'
Usage: tools/prod/ecommerce-deploy.sh <phase> [--env prod] [--dry-run] [--tag TAG] [--skip-smoke] [--upload-prod-config]
Phases: build, upload, restart, drift-check, smoke, evidence, all
Preserves remote prod config by default. Prints no secrets.
USAGE
      exit 0
      ;;
    *) fail "unknown_arg arg=$1" ;;
  esac
  shift || true
done

load_topology
TS=$(date +%Y%m%d-%H%M%S)
LOCAL_PROD_DIR="$REPO_ROOT/artifacts/prod"
EVIDENCE_DIR="$LOCAL_PROD_DIR/evidence"
STATE_DIR="$LOCAL_PROD_DIR/.deploy-state"
mkdir -p "$LOCAL_PROD_DIR" "$EVIDENCE_DIR" "$STATE_DIR"
TAG_STATE="$STATE_DIR/tag"
OUT_STATE="$STATE_DIR/image_tar"
METHOD_STATE="$STATE_DIR/build_method"
if [ -z "$PROD_TAG_IN" ]; then
  if [ "$PHASE" != "build" ] && [ "$PHASE" != "all" ] && [ -s "$TAG_STATE" ]; then
    PROD_TAG_IN="$(cat "$TAG_STATE")"
  else
    PROD_TAG_IN="prod-$TS"
  fi
fi
if ! printf '%s' "$PROD_TAG_IN" | grep -Eq '^[A-Za-z0-9_.-]{1,128}$'; then
  fail "unsafe_deploy_tag"
fi
IMG="$ECOMMERCE_IMAGE:$PROD_TAG_IN"
OUT_DEFAULT="$LOCAL_PROD_DIR/${ECOMMERCE_IMAGE##*/}_prod_$TS.tar.gz"
if [ "$DRY_RUN" != "1" ]; then
  printf '%s\n' "$PROD_TAG_IN" > "$TAG_STATE"
fi

require_clean_commit() {
  (cd "$REPO_ROOT" && git rev-parse --verify HEAD >/dev/null) || fail "build_preflight_missing_git_commit"
  local dirty
  dirty="$(cd "$REPO_ROOT" && git status --porcelain=v1)"
  if [ -n "$dirty" ]; then
    echo "$dirty" >&2
    fail "build_preflight_dirty_worktree commit_or_stash_before_build"
  fi
  GIT_SHA="$(cd "$REPO_ROOT" && git rev-parse HEAD)"
  GIT_SHORT_SHA="$(cd "$REPO_ROOT" && git rev-parse --short=12 HEAD)"
  GIT_BRANCH="$(cd "$REPO_ROOT" && git rev-parse --abbrev-ref HEAD)"
  BUILD_CREATED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log "BUILD_PREFLIGHT_PASS branch=$GIT_BRANCH sha=$GIT_SHA clean=true"
}
image_label_args() {
  printf '%s
' \
    --label "org.opencontainers.image.revision=$GIT_SHA" \
    --label "org.opencontainers.image.source=ecommerce-backend" \
    --label "org.opencontainers.image.created=$BUILD_CREATED" \
    --label "org.opencontainers.image.version=$GIT_SHORT_SHA" \
    --label "com.agent.git.branch=$GIT_BRANCH"
}

placeholder_scan() {
  local file="$1"
  if grep -Eiq '(change-me|changeme|placeholder|example-secret|your-secret)' "$file"; then
    fail "prod_config_placeholder_detected path=$file set UPLOAD_PROD_CONFIG=0 or fix config"
  fi
}

build_phase() {
  if [ "$DRY_RUN" = "1" ]; then
    log "DRY_RUN deploy.build image=$IMG out=$OUT_DEFAULT goproxy=${GOPROXY:-$DEFAULT_GOPROXY} fallback=local-binary"
    return 0
  fi
  require_clean_commit
  require_cmd docker
  require_cmd go
  local out="$OUT_DEFAULT"
  local method="docker"
  log "BUILD start image=$IMG"
  local build_args=()
  build_args+=(--build-arg "GOPROXY=${GOPROXY:-$DEFAULT_GOPROXY}")
  if [ -n "${DOCKER_BUILD_ARGS:-}" ]; then
    # shellcheck disable=SC2206
    build_args+=($DOCKER_BUILD_ARGS)
  fi
  if docker buildx build --platform linux/amd64 "${build_args[@]}" $(image_label_args) -t "$IMG" "$REPO_ROOT"; then
    method="docker"
  else
    warn "docker_build_failed fallback=local-binary"
    method="local-binary"
    (cd "$REPO_ROOT" && GOPROXY="${GOPROXY:-$DEFAULT_GOPROXY}" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o ecommerce-service-linux ./cmd/server)
    docker build $(image_label_args) -f "$REPO_ROOT/Dockerfile.local-binary" -t "$IMG" "$REPO_ROOT"
    rm -f "$REPO_ROOT/ecommerce-service-linux"
  fi
  docker save "$IMG" | gzip > "$out"
  printf '%s\n' "$out" > "$OUT_STATE"
  printf '%s\n' "$method" > "$METHOD_STATE"
  log "BUILD complete image=$IMG out=$out method=$method"
}

upload_phase() {
  local out="${IMAGE_TAR_FILE:-}"
  if [ -z "$out" ]; then
    if [ "$DRY_RUN" = "1" ] && { [ "$PHASE" = "build" ] || [ "$PHASE" = "all" ]; }; then
      out="$OUT_DEFAULT"
    else
      out="$(cat "$OUT_STATE" 2>/dev/null || true)"
    fi
  fi
  if [ "$DRY_RUN" = "1" ]; then
    [ -n "$out" ] || out="$OUT_DEFAULT"
    local base
    base="$(basename "$out")"
    log "DRY_RUN deploy.upload artifact=$out remote=$REMOTE:$REMOTE_BASE/$base upload_prod_config=$UPLOAD_PROD_CONFIG"
    return 0
  fi
  [ -n "$out" ] || fail "missing_build_artifact run_phase=build"
  local base
  base="$(basename "$out")"
  if [ "$UPLOAD_PROD_CONFIG" = "1" ]; then
    placeholder_scan "$REPO_ROOT/config.prod.yaml"
  fi
  remote_cmd "mkdir -p '$REMOTE_DIR' '$REMOTE_BASE' '$REMOTE_BASE/backups/ecommerce-prod'"
  scp -i "$SSH_KEY" "$out" "$REMOTE:$REMOTE_BASE/"
  scp -i "$SSH_KEY" "$REPO_ROOT/docker-compose.yml" "$REMOTE:$REMOTE_DIR/"
  if [ "$UPLOAD_PROD_CONFIG" = "1" ]; then
    scp -i "$SSH_KEY" "$REPO_ROOT/config.prod.yaml" "$REMOTE:$REMOTE_DIR/config.prod.yaml"
    warn "prod_config_uploaded explicit_opt_in=true"
  else
    log "SKIP prod_config_upload reason=default_preserve_remote_config"
  fi
  log "UPLOAD complete artifact=$base"
}

restart_phase() {
  local out=""
  if [ "$DRY_RUN" = "1" ] && { [ "$PHASE" = "build" ] || [ "$PHASE" = "all" ]; }; then
    out="$OUT_DEFAULT"
  else
    out="$(cat "$OUT_STATE" 2>/dev/null || true)"
  fi
  if [ "$DRY_RUN" = "1" ]; then
    [ -n "$out" ] || out="$OUT_DEFAULT"
    local base="$(basename "$out")"
    log "DRY_RUN deploy.restart tag=$PROD_TAG_IN artifact=$base service=${ECOMMERCE_SERVICE:-prod-backend}"
    return 0
  fi
  [ -n "$out" ] || fail "missing_build_artifact run_phase=build"
  local base="$(basename "$out")"
  remote_cmd "set -e; cd '$REMOTE_DIR'; docker load -i '$REMOTE_BASE/$base'; mkdir -p '$REMOTE_BASE/backups/ecommerce-prod'; mv -f '$REMOTE_BASE/$base' '$REMOTE_BASE/backups/ecommerce-prod/'; PROD_TAG='$PROD_TAG_IN' docker compose up -d '${ECOMMERCE_SERVICE:-prod-backend}'"
  remote_cmd "for i in \$(seq 1 60); do s=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' '${ECOMMERCE_CONTAINER:-v-ecommerce-backend}' 2>/dev/null || echo none); [ \"\$s\" = healthy ] && echo 'PASS container_healthy name=${ECOMMERCE_CONTAINER:-v-ecommerce-backend}' && exit 0; sleep 2; done; echo 'FAIL container_health_timeout name=${ECOMMERCE_CONTAINER:-v-ecommerce-backend}'; exit 1"
}

drift_phase() {
  local args=(--env prod --fail-on-critical)
  if [ "$DRY_RUN" = "1" ]; then args+=(--dry-run); fi
  if [ "$DRY_RUN" = "1" ]; then
    "$SCRIPT_DIR/ecommerce-drift-check.sh" "${args[@]}"
    return $?
  fi
  local log_file="$EVIDENCE_DIR/ecommerce-drift-${PROD_TAG_IN}.log"
  set +e
  "$SCRIPT_DIR/ecommerce-drift-check.sh" "${args[@]}" 2>&1 | tee "$log_file"
  local status=${PIPESTATUS[0]}
  set -e
  log "EVIDENCE drift_log=$log_file"
  return "$status"
}

smoke_phase() {
  if [ "$SKIP_SMOKE" = "1" ]; then
    warn "smoke_skipped explicit_skip=true"
    return 0
  fi
  local args=(--env prod --fail-on-critical)
  if [ "$DRY_RUN" = "1" ]; then args+=(--dry-run); fi
  if [ "$DRY_RUN" = "1" ]; then
    "$SCRIPT_DIR/ecommerce-visual-workflow-smoke.sh" "${args[@]}"
    return $?
  fi
  local log_file="$EVIDENCE_DIR/ecommerce-visual-workflow-smoke-${PROD_TAG_IN}.log"
  set +e
  "$SCRIPT_DIR/ecommerce-visual-workflow-smoke.sh" "${args[@]}" 2>&1 | tee "$log_file"
  local status=${PIPESTATUS[0]}
  set -e
  log "EVIDENCE smoke_log=$log_file"
  return "$status"
}

evidence_phase() {
  local file="$EVIDENCE_DIR/ecommerce-backend_${PROD_TAG_IN}.md"
  if [ "$DRY_RUN" = "1" ]; then
    log "DRY_RUN deploy.evidence path=$file"
    return 0
  fi
  local method="$(cat "$METHOD_STATE" 2>/dev/null || echo unknown)"
  write_evidence_header "$file" "Ecommerce prod deploy evidence"
  {
    echo "## Deployment"
    echo "- image: \`$IMG\`"
    echo "- tag: \`$PROD_TAG_IN\`"
    echo "- build_method: \`$method\`"
    echo "- remote: \`$REMOTE\`"
    echo "- remote_dir: \`$REMOTE_DIR\`"
    echo "- prod_config_uploaded: \`$UPLOAD_PROD_CONFIG\`"
    echo
    echo "## Required follow-up evidence"
    echo "- drift_check: run \`tools/prod/ecommerce-drift-check.sh --env prod --fail-on-critical\`"
    echo "- smoke: run \`tools/prod/ecommerce-visual-workflow-smoke.sh --env prod --fail-on-critical\`"
    echo
    echo "## Captured logs"
    if [ -f "$EVIDENCE_DIR/ecommerce-drift-${PROD_TAG_IN}.log" ]; then
      echo "- drift_log: \`$EVIDENCE_DIR/ecommerce-drift-${PROD_TAG_IN}.log\`"
      tail -n 5 "$EVIDENCE_DIR/ecommerce-drift-${PROD_TAG_IN}.log" | sed 's/^/  /'
    else
      echo "- drift_log: not captured in this run"
    fi
    if [ -f "$EVIDENCE_DIR/ecommerce-visual-workflow-smoke-${PROD_TAG_IN}.log" ]; then
      echo "- smoke_log: \`$EVIDENCE_DIR/ecommerce-visual-workflow-smoke-${PROD_TAG_IN}.log\`"
      tail -n 5 "$EVIDENCE_DIR/ecommerce-visual-workflow-smoke-${PROD_TAG_IN}.log" | sed 's/^/  /'
    else
      echo "- smoke_log: not captured in this run"
    fi
  } >> "$file"
  log "EVIDENCE path=$file"
}

case "$PHASE" in
  build) build_phase ;;
  upload) upload_phase ;;
  restart) restart_phase ;;
  drift-check) drift_phase ;;
  smoke) smoke_phase ;;
  evidence) evidence_phase ;;
  all)
    build_phase
    upload_phase
    restart_phase
    drift_phase
    smoke_phase
    evidence_phase
    ;;
  *) fail "unknown_phase phase=$PHASE" ;;
esac
