#!/usr/bin/env bash
set -Eeuo pipefail

read -r SUDO_PASS

sudo_cmd() { printf '%s\n' "$SUDO_PASS" | sudo -S -p '' "$@"; }

upsert_env() {
  local key="$1" value="$2" file="$3"
  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$file"
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

PROJECT=/home/lxy/lpicto
ARCHIVE=/home/lxy/lpicto-deploy.tgz
TS=$(date +%Y%m%d%H%M%S)
STAGING=/home/lxy/lpicto.next.$TS
BACKUP=/home/lxy/lpicto.backup.$TS
BUILD_LOG=/home/lxy/lpicto-deploy-build.log
export COMPOSE_PROGRESS=plain
export DOCKER_BUILDKIT=1

echo "deploy_ts=$TS"

test -f "$ARCHIVE" || { echo 'missing_archive'; exit 10; }

test -e /dev/dri/renderD128 || { echo 'missing_gpu_device=/dev/dri/renderD128'; exit 12; }

RENDER_GID=$(getent group render | cut -d: -f3 || true)
VIDEO_GID=$(getent group video | cut -d: -f3 || true)
: "${RENDER_GID:=109}"
: "${VIDEO_GID:=44}"
echo "gpu=/dev/dri/renderD128 render_gid=$RENDER_GID video_gid=$VIDEO_GID"

sudo_cmd docker info --format 'docker_server={{.ServerVersion}}'

rm -rf "$STAGING"
mkdir -p "$STAGING"
tar -xzf "$ARCHIVE" -C "$STAGING"

# --- stop existing stack ---
if [ -d "$PROJECT" ] && [ -f "$PROJECT/docker-compose.yml" ]; then
  echo 'stopping_existing_stack=1'
  cd "$PROJECT"
  if [ -f docker-compose.gpu.yml ]; then
    sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml down --remove-orphans >/tmp/lpicto-compose-down.$TS.log 2>&1 \
      || sudo_cmd docker compose down --remove-orphans >/tmp/lpicto-compose-down-fallback.$TS.log 2>&1 || true
  else
    sudo_cmd docker compose down --remove-orphans >/tmp/lpicto-compose-down.$TS.log 2>&1 || true
  fi
fi

# --- preserve .env ---
if [ -f "$PROJECT/.env" ]; then
  cp "$PROJECT/.env" "$STAGING/.env"
else
  cat > "$STAGING/.env" <<'ENVEOF'
LPICTO_BIND=0.0.0.0
LPICTO_PORT=18080
LPICTO_MEDIA=/mnt
LPICTO_DATA=./data/app
LPICTO_CACHE=./data/cache
GOPROXY=https://goproxy.cn,direct
NPM_CONFIG_REGISTRY=https://registry.npmmirror.com
APT_MIRROR=http://mirrors.tuna.tsinghua.edu.cn/debian
APT_SECURITY_MIRROR=http://mirrors.tuna.tsinghua.edu.cn/debian-security
ENVEOF
fi

upsert_env LPICTO_VIDEO_GID "$VIDEO_GID" "$STAGING/.env"
upsert_env LPICTO_RENDER_GID "$RENDER_GID" "$STAGING/.env"
upsert_env LIBVA_DRIVER_NAME iHD "$STAGING/.env"

# --- preserve data ---
if [ -d "$PROJECT/data" ]; then
  echo 'preserve_data=move_existing_project_data'
  rm -rf "$STAGING/data"
  mv "$PROJECT/data" "$STAGING/data"
fi
mkdir -p "$STAGING/data/app" "$STAGING/data/cache"

# --- swap ---
if [ -d "$PROJECT" ]; then
  mv "$PROJECT" "$BACKUP"
  echo "backup=$BACKUP"
fi
mv "$STAGING" "$PROJECT"

# --- build & start ---
cd "$PROJECT"

echo 'compose_config_check=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml config >/tmp/lpicto-compose-config.$TS.yml

echo 'compose_up_build=1'
if ! sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build >"$BUILD_LOG" 2>&1; then
  echo 'compose_up_failed=1'
  tail -n 160 "$BUILD_LOG"
  exit 20
fi

echo 'compose_ps=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml ps

# --- health check ---
echo 'health_wait=1'
health_ok=0
for i in $(seq 1 90); do
  if curl -fsS http://127.0.0.1:18080/api/health; then
    echo
    health_ok=1
    break
  fi
  sleep 2
done

if [ "$health_ok" != 1 ]; then
  echo 'health_failed=1'
  sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml logs --tail=120 api worker nginx
  exit 21
fi

# --- GPU check ---
echo 'gpu_check=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml exec -T api sh -lc \
  'ls -l /dev/dri/renderD128 && ffmpeg -hide_banner -encoders | grep -E "h264_vaapi" | head -n 3'

echo 'api_worker_hwaccel=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml logs --tail=80 api worker \
  | grep -E 'ffmpeg hardware acceleration selected|ffmpeg hardware acceleration unavailable' || true

echo 'deploy_done=1'
