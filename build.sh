#!/bin/sh
set -e
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
  scp -i "$SSH_KEY" "$OUT_FILE" "$REMOTE:$REMOTE_BASE/"
  scp -i "$SSH_KEY" docker-compose.yml "$REMOTE:$REMOTE_DIR/"
  [ -f config.prod.yaml ] && scp -i "$SSH_KEY" config.prod.yaml "$REMOTE:$REMOTE_DIR/config.prod.yaml"
  [ -f config.dev.yaml ] && scp -i "$SSH_KEY" config.dev.yaml "$REMOTE:$REMOTE_DIR/config.dev.yaml"
}

remote() { ssh "$REMOTE" -i "$SSH_KEY" "$1"; }
health_wait() {
  NAME="$1"; LIM="$2"
  remote "for i in \$(seq 1 $LIM); do s=\$(docker inspect -f '{{.State.Health.Status}}' $NAME 2>/dev/null || echo none); [ "\$s" = "healthy" ] && echo HEALTHY && exit 0; sleep 2; done; echo HEALTH_CHECK_FAILED; exit 1"
}

case "$CMD" in
  dev)
    DEV_TAG="${DEV_TAG:-dev}"
    IMG="$IMAGE_NAME:$DEV_TAG"
    mkdir -p "$LOCAL_DEV_DIR"
    OUT="$LOCAL_DEV_DIR/${IMAGE_NAME##*/}_dev_$TS.tar.gz"
    REMOTE_OUT="$REMOTE_BASE/$(basename "$OUT")"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ecommerce-service-linux ./cmd/server
    docker buildx build --platform linux/amd64 -f Dockerfile.dev -t "$IMG" .
    docker save "$IMG" | gzip > "$OUT"
    send_files "$OUT"
    remote "docker load -i $REMOTE_OUT"
    remote "mkdir -p $REMOTE_BASE/backups/ecommerce-dev; mv -f $REMOTE_OUT $REMOTE_BASE/backups/ecommerce-dev/"
    remote "cd $REMOTE_DIR; DEV_TAG=$DEV_TAG docker compose up -d dev-backend"
    health_wait "v-ecommerce-backend-dev" 60
    ;;
  prod)
    PROD_TAG_IN="${PROD_TAG:-prod-$TS}"
    IMG="$IMAGE_NAME:$PROD_TAG_IN"
    mkdir -p "$LOCAL_PROD_DIR"
    OUT="$LOCAL_PROD_DIR/${IMAGE_NAME##*/}_prod_$TS.tar.gz"
    REMOTE_OUT="$REMOTE_BASE/$(basename "$OUT")"
    docker buildx build --platform linux/amd64 -t "$IMG" .
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
