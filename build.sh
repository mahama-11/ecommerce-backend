#!/bin/sh
set -e
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$SCRIPT_DIR"
CMD="$1"
IMAGE_NAME="${IMAGE_NAME:-ver/v-ecommerce-backend}"
REMOTE="${REMOTE:-root@159.138.228.40}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/KeyPair-v2.pem}"
REMOTE_DIR="${REMOTE_DIR:-/root/gk/ecommerce-backend}"
TS=$(date +%Y%m%d-%H%M%S)
REMOTE_BASE="${REMOTE_DIR%/*}"

LOCAL_DEV_DIR="artifacts/dev"
LOCAL_PROD_DIR="artifacts/prod"

send_files() {
  OUT_FILE="$1"
  ssh -i "$SSH_KEY" "$REMOTE" "mkdir -p '$REMOTE_DIR' '$REMOTE_BASE'"
  scp -i "$SSH_KEY" "$OUT_FILE" "$REMOTE:$REMOTE_BASE/"
  scp -i "$SSH_KEY" docker-compose.yml "$REMOTE:$REMOTE_DIR/"
  [ -f config.prod.yaml ] && scp -i "$SSH_KEY" config.prod.yaml "$REMOTE:$REMOTE_DIR/config.prod.yaml"
  [ -f config.dev.yaml ] && scp -i "$SSH_KEY" config.dev.yaml "$REMOTE:$REMOTE_DIR/config.dev.yaml"
}

remote() { ssh -i "$SSH_KEY" "$REMOTE" "$1"; }
require_clean_commit() {
  git rev-parse --verify HEAD >/dev/null || { echo "BUILD_PREFLIGHT_FAIL: missing git commit" >&2; exit 1; }
  dirty="$(git status --porcelain=v1)"
  if [ -n "$dirty" ]; then
    echo "BUILD_PREFLIGHT_FAIL: working tree must be clean; commit or stash changes before build/deploy" >&2
    printf '%s
' "$dirty" >&2
    exit 1
  fi
  GIT_SHA="$(git rev-parse HEAD)"
  GIT_SHORT_SHA="$(git rev-parse --short=12 HEAD)"
  GIT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
  BUILD_CREATED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "BUILD_PREFLIGHT_PASS branch=$GIT_BRANCH sha=$GIT_SHA clean=true"
}
image_labels() {
  printf '%s
' \
    --label "org.opencontainers.image.revision=$GIT_SHA" \
    --label "org.opencontainers.image.source=ecommerce-backend" \
    --label "org.opencontainers.image.created=$BUILD_CREATED" \
    --label "org.opencontainers.image.version=$GIT_SHORT_SHA" \
    --label "com.agent.git.branch=$GIT_BRANCH"
}
health_wait() {
  NAME="$1"; LIM="$2"
  remote "for i in \$(seq 1 $LIM); do s=\$(docker inspect -f '{{.State.Health.Status}}' $NAME 2>/dev/null || echo none); [ "\$s" = "healthy" ] && echo HEALTHY && exit 0; sleep 2; done; echo HEALTH_CHECK_FAILED; exit 1"
}

case "$CMD" in
  dev)
    require_clean_commit
    DEV_TAG="${DEV_TAG:-dev}"
    IMG="$IMAGE_NAME:$DEV_TAG"
    mkdir -p "$LOCAL_DEV_DIR"
    OUT="$LOCAL_DEV_DIR/${IMAGE_NAME##*/}_dev_$TS.tar.gz"
    REMOTE_OUT="$REMOTE_BASE/$(basename "$OUT")"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ecommerce-service-linux ./cmd/server
    docker buildx build --platform linux/amd64 $(image_labels) -f Dockerfile.dev -t "$IMG" .
    docker save "$IMG" | gzip > "$OUT"
    send_files "$OUT"
    remote "docker load -i $REMOTE_OUT"
    remote "mkdir -p $REMOTE_BASE/backups/ecommerce-dev; mv -f $REMOTE_OUT $REMOTE_BASE/backups/ecommerce-dev/"
    remote "cd $REMOTE_DIR; DEV_TAG=$DEV_TAG docker compose up -d dev-backend"
    health_wait "v-ecommerce-backend-dev" 60
    ;;
  prod)
    require_clean_commit
    PROD_TAG_IN="${PROD_TAG:-prod-$TS}"
    IMG="$IMAGE_NAME:$PROD_TAG_IN"
    mkdir -p "$LOCAL_PROD_DIR"
    OUT="$LOCAL_PROD_DIR/${IMAGE_NAME##*/}_prod_$TS.tar.gz"
    REMOTE_OUT="$REMOTE_BASE/$(basename "$OUT")"
    docker buildx build --platform linux/amd64 $(image_labels) -t "$IMG" .
    docker save "$IMG" | gzip > "$OUT"
    send_files "$OUT"
    remote "docker load -i $REMOTE_OUT"
    remote "mkdir -p $REMOTE_BASE/backups/ecommerce-prod; mv -f $REMOTE_OUT $REMOTE_BASE/backups/ecommerce-prod/"
    remote "cd $REMOTE_DIR; PROD_TAG=$PROD_TAG_IN docker compose up -d prod-backend"
    health_wait "v-ecommerce-backend" 60
    ;;
  *)
    echo "Usage: ./build.sh [dev|prod]"
    exit 1
    ;;
esac
