<#
.SYNOPSIS
    lPicto 一键部署：打包 -> 上传 -> 远端 Docker 重建并启动
    Docker 在远端编译（Dockerfile 多阶段构建：前端 npm build + go build），本地无需 Node/Go。
    右键 -> "使用 PowerShell 运行"，或从终端执行 .\deploy.ps1
#>

param(
    [string]$HostIP   = "192.168.2.97",
    [string]$User     = "lxy",
    [string]$Password = "lxylxylxy"
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

# ── helpers ─────────────────────────────────────────────────────
function Write-Step { Write-Host "`n── $args" -ForegroundColor Cyan }
function Write-OK   { Write-Host "   ✔ $args" -ForegroundColor Green }
function Write-Err  { Write-Host "   ✘ $args" -ForegroundColor Red; exit 1 }

$sshOpts = '-o', 'StrictHostKeyChecking=no', '-o', 'BatchMode=no', '-o', 'ConnectTimeout=10'
$remote  = "${User}@${HostIP}"
$remoteTgz = '/home/lxy/lpicto-deploy.tgz'
$remoteSh  = '/home/lxy/lpicto-deploy-run.sh'
$appPort   = 18080

# ── preflight ───────────────────────────────────────────────────
Write-Step "检查前置条件"
if (-not (Get-Command ssh  -ErrorAction SilentlyContinue)) { Write-Err "未找到 ssh  — 请安装 OpenSSH Client" }
if (-not (Get-Command scp  -ErrorAction SilentlyContinue)) { Write-Err "未找到 scp  — 请安装 OpenSSH Client" }

# 确保 sshpass 可用（没有它无法自动输入密码）
$sshpassCmd = Get-Command sshpass -ErrorAction SilentlyContinue
if (-not $sshpassCmd) {
    Write-Host "   未找到 sshpass，尝试自动安装..." -ForegroundColor DarkGray
    # 尝试 winget 的几个已知包名
    $ids = @('sshpass', 'GnuWin32.SshPass')
    foreach ($id in $ids) {
        winget install --silent --accept-source-agreements --accept-package-agreements $id 2>$null
        Start-Sleep -Seconds 2
        $env:Path = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" +
                     [System.Environment]::GetEnvironmentVariable("Path","User")
        if (Get-Command sshpass -ErrorAction SilentlyContinue) { break }
    }
    $sshpassCmd = Get-Command sshpass -ErrorAction SilentlyContinue
}
if (-not $sshpassCmd) {
    Write-Err @"
sshpass 自动安装失败。请手动执行以下任一操作后重新运行本脚本：

  方式1 (winget):    winget install sshpass
  方式2 (下载):      从 https://github.com/kevinburke/sshpass/releases 下载 sshpass.exe 放到 PATH 中
"@
}
Write-OK "ssh / scp / sshpass 可用"

# ── 1. 打包 ─────────────────────────────────────────────────────
Write-Step "1/4  打包项目（排除 .git data cache node_modules dist .env db 等）"

$archive = Join-Path $PSScriptRoot 'lpicto-deploy.tgz'
Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue

$excludes = @(
    './.git'
    './data'
    './cache'
    './frontend/node_modules'
    './frontend/dist'
    './frontend/tsconfig.tsbuildinfo'
    './.env'
    '*.db'
    '*.db-shm'
    '*.db-wal'
    './.codex'
    './.agents'
    './.gitignore'
    './.gitattributes'
    './.dockerignore'
    './lpicto-deploy.tgz'
    './deploy.ps1'
    './remote-deploy.sh'
)

$tarArgs = @('-czf', $archive) + ($excludes | ForEach-Object { '--exclude'; $_ }) + @('.')
& tar @tarArgs 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Err "tar 打包失败" }

$size = [math]::Round((Get-Item $archive).Length / 1KB)
Write-OK "归档完成  ${size} KB"

# ── 2. 上传 ─────────────────────────────────────────────────────
Write-Step "2/4  上传归档到 ${HostIP}"

$env:SSHPASS = $Password
& sshpass -e scp @sshOpts $archive "${remote}:${remoteTgz}"
if ($LASTEXITCODE -ne 0) { Write-Err "scp 上传失败" }
Write-OK "归档已上传"

# ── 3. 构建远端脚本并上传 ──────────────────────────────────────
Write-Step "3/4  推送远端部署脚本"

$remoteBash = @'
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

if [ -d "$PROJECT/data" ]; then
  echo 'preserve_data=move_existing_project_data'
  rm -rf "$STAGING/data"
  mv "$PROJECT/data" "$STAGING/data"
fi
mkdir -p "$STAGING/data/app" "$STAGING/data/cache"

if [ -d "$PROJECT" ]; then
  mv "$PROJECT" "$BACKUP"
  echo "backup=$BACKUP"
fi
mv "$STAGING" "$PROJECT"

cd "$PROJECT"

echo 'compose_config_check=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml config >/tmp/lpicto-compose-config.$TS.yml

echo 'compose_up_build=1'
echo "Build output -> $BUILD_LOG"
if ! sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build >"$BUILD_LOG" 2>&1; then
  echo 'compose_up_failed=1'
  tail -n 160 "$BUILD_LOG"
  exit 20
fi

echo 'compose_ps=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml ps

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

echo 'gpu_check=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml exec -T api sh -lc \
  'ls -l /dev/dri/renderD128 && ffmpeg -hide_banner -encoders | grep -E "h264_vaapi" | head -n 3'

echo 'api_worker_hwaccel=1'
sudo_cmd docker compose -f docker-compose.yml -f docker-compose.gpu.yml logs --tail=80 api worker \
  | grep -E 'ffmpeg hardware acceleration selected|ffmpeg hardware acceleration unavailable' || true

echo 'deploy_done=1'
'@

$localSh = Join-Path $env:TEMP 'lpicto-deploy-run.sh'
$remoteBash -replace "`r", "" | Set-Content -Path $localSh -NoNewline

& sshpass -e scp @sshOpts $localSh "${remote}:${remoteSh}"
if ($LASTEXITCODE -ne 0) { Write-Err "远端脚本上传失败" }
Remove-Item $localSh -Force
Write-OK "远端脚本已推送"

# ── 4. 执行远端部署 ────────────────────────────────────────────
Write-Step "4/4  远端 Docker 重建并启动（编译 + 启动，需要几分钟）"

$Password | & sshpass -e ssh @sshOpts $remote "bash ${remoteSh}"
if ($LASTEXITCODE -ne 0) { Write-Err "远端部署失败"; exit $LASTEXITCODE }

# ── 5. 本地验证 ─────────────────────────────────────────────────
Write-Step "本地验证 http://${HostIP}:${appPort}/api/health"
try {
    $health = Invoke-RestMethod -Uri "http://${HostIP}:${appPort}/api/health" -TimeoutSec 5
    Write-OK "健康检查通过: $($health | ConvertTo-Json -Compress)"
} catch {
    Write-Host "   (本地无法直连 ${HostIP}:${appPort}，远端健康检查已在上一步完成)" -ForegroundColor DarkGray
}

Write-Host "`n部署完成。入口: http://${HostIP}:${appPort}" -ForegroundColor Green

# 清理
Remove-Item $archive -Force -ErrorAction SilentlyContinue
