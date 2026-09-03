<#
.SYNOPSIS
    lPicto 一键部署到 Ubuntu。

.DESCRIPTION
    自动打包当前工作区、上传到 Ubuntu、编译并切换 Docker Compose 服务，
    最后检查健康状态和 CPU 转码。远端 .env、数据库、缓存和媒体配置均会保留。
#>

param(
    [string]$HostIP = "192.168.2.97",
    [string]$User = "lxy",
    [string]$Password = "lxylxylxy",
    [int]$Port = 18080
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

function Write-Step { Write-Host "`n-- $args" -ForegroundColor Cyan }
function Write-OK { Write-Host "   完成：$args" -ForegroundColor Green }
function Stop-Deploy([string]$Message) { throw $Message }

$sshOptions = @('-o', 'StrictHostKeyChecking=no', '-o', 'ConnectTimeout=10')
$remote = "${User}@${HostIP}"
$remoteHome = "/home/${User}"
$remoteArchive = "${remoteHome}/lpicto-deploy.tgz"
$remoteScript = "${remoteHome}/lpicto-deploy-run.sh"
$archive = Join-Path $PSScriptRoot 'lpicto-deploy.tgz'
$localRunner = Join-Path ([System.IO.Path]::GetTempPath()) 'lpicto-deploy-run.sh'
$localAIModels = Join-Path $env:LOCALAPPDATA 'lPicto\ai-models'
$externalAITokenPath = Join-Path $env:LOCALAPPDATA 'lPicto\external-ai-token'
$aiModelFiles = @(
    @{ Rel = 'Qwen3VL-8B-Instruct-Q4_K_M.gguf'; Size = 5027784800L; SHA = '67d1659bfe71b89d50b45a4ad1a9e5b997e5bb16ce5da66a6a6167abd569e9e2'; URL = 'https://huggingface.co/Qwen/Qwen3-VL-8B-Instruct-GGUF/resolve/f982a07559d4a2f6c8744d840bf6fccab30eea96/Qwen3VL-8B-Instruct-Q4_K_M.gguf' },
    @{ Rel = 'mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf'; Size = 752289728L; SHA = 'c6ba85508d82f42590e6eb77d5340369ab6fecf107a7561d809523d8aa5f3bfd'; URL = 'https://huggingface.co/Qwen/Qwen3-VL-8B-Instruct-GGUF/resolve/f982a07559d4a2f6c8744d840bf6fccab30eea96/mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf' },
    @{ Rel = 'chinese-clip/onnx/model.onnx'; Size = 753665706L; SHA = 'd4e282affd5f09e196856cc63fbd0e77c576f598fdf6f6bb78ee61f1ef7cd770'; URL = 'https://huggingface.co/Xenova/chinese-clip-vit-base-patch16/resolve/f26904860903e70e050b8f48255e5f48401816e9/onnx/model.onnx' }
)

function Test-LockedFile($Path, $Size, $SHA) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $false }
    if ((Get-Item -LiteralPath $Path).Length -ne $Size) { return $false }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() -eq $SHA
}

function Prepare-LocalAIModels {
    foreach ($model in $aiModelFiles) {
        $target = Join-Path $localAIModels $model.Rel
        if (Test-LockedFile $target $model.Size $model.SHA) { continue }
        New-Item -ItemType Directory -Force (Split-Path $target) | Out-Null
        $part = "$target.part"
        if ((Test-Path -LiteralPath $part) -and (Get-Item -LiteralPath $part).Length -gt $model.Size) {
            Remove-Item -LiteralPath $part -Force
        }
        Write-Host "   下载 $($model.Rel) ($($model.Size) 字节)"
        & curl.exe --location --fail --retry 5 --retry-all-errors --continue-at - --output $part $model.URL
        if ($LASTEXITCODE -ne 0) { Stop-Deploy "模型下载失败：$($model.Rel)" }
        if (-not (Test-LockedFile $part $model.Size $model.SHA)) { Stop-Deploy "模型校验失败：$($model.Rel)" }
        Move-Item -LiteralPath $part -Destination $target -Force
    }
    $clipBase = 'https://huggingface.co/Xenova/chinese-clip-vit-base-patch16/resolve/f26904860903e70e050b8f48255e5f48401816e9'
    foreach ($rel in @('config.json', 'preprocessor_config.json', 'tokenizer.json', 'tokenizer_config.json', 'special_tokens_map.json', 'vocab.txt')) {
        $target = Join-Path $localAIModels "chinese-clip/$rel"
        if (Test-Path -LiteralPath $target -PathType Leaf) { continue }
        New-Item -ItemType Directory -Force (Split-Path $target) | Out-Null
        Invoke-WebRequest -Uri "$clipBase/$rel" -OutFile $target -MaximumRedirection 8
    }
}
function Get-ExternalAIToken {
    New-Item -ItemType Directory -Force (Split-Path $externalAITokenPath) | Out-Null
    if (-not (Test-Path -LiteralPath $externalAITokenPath -PathType Leaf)) {
        $bytes = New-Object byte[] 32
        [Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
        [IO.File]::WriteAllText($externalAITokenPath, [Convert]::ToHexString($bytes).ToLowerInvariant())
    }
    $token = ([IO.File]::ReadAllText($externalAITokenPath)).Trim()
    if ($token -notmatch '^[0-9a-f]{64}$') { Stop-Deploy "外部 AI 认证密钥无效：$externalAITokenPath" }
    return $token
}
$remoteRunner = @'
#!/usr/bin/env bash
set -Eeuo pipefail

read -r SUDO_PASS
read -r EXTERNAL_AI_TOKEN

PROJECT=$HOME/lpicto
MODEL_UPLOAD=$HOME/lpicto-ai-models-upload
ARCHIVE=$HOME/lpicto-deploy.tgz
TS=$(date +%Y%m%d%H%M%S)
STAGING=$HOME/lpicto.next.$TS
BACKUP=$HOME/lpicto.backup.$TS
BUILD_LOG=$HOME/lpicto-deploy-build.log
COMPOSE_FILES=(-f docker-compose.yml -f docker-compose.gpu.yml)
export COMPOSE_PROJECT_NAME=lpicto
export COMPOSE_PROGRESS=plain
export DOCKER_BUILDKIT=1

docker_cmd() { docker "$@"; }
if ! docker info >/dev/null 2>&1; then
  if ! printf '%s\n' "$SUDO_PASS" | sudo -S -p '' docker info >/dev/null 2>&1; then
    echo '错误：当前用户和 sudo 均无法访问 Docker'
    exit 11
  fi
  docker_cmd() { printf '%s\n' "$SUDO_PASS" | sudo -S -p '' docker "$@"; }
fi

compose() {
  docker_cmd compose "${COMPOSE_FILES[@]}" "$@"
}

upsert_env() {
  local key="$1" value="$2" file="$3"
  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$file"
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

cleanup_staging() {
  if [ -d "$STAGING" ]; then
    rm -rf "$STAGING"
  fi
}
trap cleanup_staging EXIT

echo "部署时间：$TS"
test -f "$ARCHIVE" || { echo "错误：找不到上传归档 $ARCHIVE"; exit 10; }
docker_cmd info --format 'Docker 服务端版本：{{.ServerVersion}}'
echo '媒体转码：CPU / GPU 可按播放会话选择（GPU 使用 VAAPI）'

rm -rf "$STAGING"
mkdir -p "$STAGING"
tar -xzf "$ARCHIVE" -C "$STAGING"

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

upsert_env LPICTO_BIND 0.0.0.0 "$STAGING/.env"
upsert_env LPICTO_PORT 18080 "$STAGING/.env"
upsert_env LPICTO_MEDIA /mnt "$STAGING/.env"
upsert_env LPICTO_DATA ./data/app "$STAGING/.env"
upsert_env LPICTO_CACHE ./data/cache "$STAGING/.env"
upsert_env FFMPEG_HWACCEL auto "$STAGING/.env"
test -e /dev/dri/card0 || { echo '错误：找不到核显设备 /dev/dri/card0'; exit 14; }
test -e /dev/dri/renderD128 || { echo '错误：找不到核显渲染设备 /dev/dri/renderD128'; exit 14; }
upsert_env LPICTO_VIDEO_GID "$(stat -c %g /dev/dri/card0)" "$STAGING/.env"
upsert_env LPICTO_RENDER_GID "$(stat -c %g /dev/dri/renderD128)" "$STAGING/.env"
upsert_env LIVE_VIDEO_PROXY_MAX_ACTIVE 2 "$STAGING/.env"
upsert_env VIDEO_PRELOAD_SEGMENTS 2 "$STAGING/.env"
upsert_env ENABLE_FS_WATCH false "$STAGING/.env"
upsert_env FILE_COUNT_SCAN_INTERVAL_MINUTES 0 "$STAGING/.env"
upsert_env SCAN_INTERVAL_MINUTES 0 "$STAGING/.env"
upsert_env NAS_WATCHER_ROOTS 'PIC=nas/PIC;VID=nas/VID' "$STAGING/.env"
upsert_env NAS_WATCHER_OFFLINE_SECONDS 90 "$STAGING/.env"
upsert_env EXTERNAL_AI_TOKEN "$EXTERNAL_AI_TOKEN" "$STAGING/.env"
mkdir -p "$STAGING/data/app" "$STAGING/data/cache"

echo '校验本机上传的锁定版本 AI 模型（旧服务保持运行）'
test -d "$MODEL_UPLOAD" || { echo "错误：找不到模型上传目录 $MODEL_UPLOAD"; exit 13; }
cd "$MODEL_UPLOAD"
echo '67d1659bfe71b89d50b45a4ad1a9e5b997e5bb16ce5da66a6a6167abd569e9e2  Qwen3VL-8B-Instruct-Q4_K_M.gguf' | sha256sum -c -
echo 'c6ba85508d82f42590e6eb77d5340369ab6fecf107a7561d809523d8aa5f3bfd  mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf' | sha256sum -c -
echo 'd4e282affd5f09e196856cc63fbd0e77c576f598fdf6f6bb78ee61f1ef7cd770  chinese-clip/onnx/model.onnx' | sha256sum -c -
if [ -d "$PROJECT/data/app" ]; then MODEL_DEST="$PROJECT/data/app/ai-models"; else MODEL_DEST="$STAGING/data/app/ai-models"; fi
mkdir -p "$MODEL_DEST"
cp -a "$MODEL_UPLOAD/." "$MODEL_DEST/"
chmod -R a+rX "$MODEL_DEST"

echo '检查 Docker Compose 配置'
cd "$STAGING"
compose config >/tmp/lpicto-compose-config.$TS.yml

echo "开始编译镜像，完整日志：$BUILD_LOG"
if ! DOCKER_BUILDKIT=0 compose build ai >"$BUILD_LOG" 2>&1; then
  echo '错误：AI 镜像编译失败，显示最后 160 行日志'
  tail -n 160 "$BUILD_LOG"
  exit 20
fi
if ! DOCKER_BUILDKIT=1 compose build api >>"$BUILD_LOG" 2>&1; then
  echo '错误：镜像编译失败，显示最后 160 行日志'
  tail -n 160 "$BUILD_LOG"
  exit 20
fi

echo '编译完成，开始切换版本'
if [ -d "$PROJECT" ] && [ -f "$PROJECT/docker-compose.yml" ]; then
  cd "$PROJECT"
  compose down --remove-orphans >/tmp/lpicto-compose-down.$TS.log 2>&1 || true
fi

if [ -d "$PROJECT" ]; then
  mv "$PROJECT" "$BACKUP"
  echo "旧版本备份：$BACKUP"
fi
mv "$STAGING" "$PROJECT"
if [ -d "$BACKUP/data" ]; then
  rm -rf "$PROJECT/data"
  mv "$BACKUP/data" "$PROJECT/data"
fi
mkdir -p "$PROJECT/data/app" "$PROJECT/data/cache"
trap - EXIT
echo '修复共享缓存目录权限'
docker_cmd run --rm -v "$PROJECT/data/cache:/cache" --entrypoint sh redis:7-alpine \
  -c 'chown -R 10001:999 /cache && chmod -R u+rwX,g+rwX,o-rwx /cache'
rm -f "$ARCHIVE"
rm -rf "$MODEL_UPLOAD"

cd "$PROJECT"
echo '启动新版本服务'
if ! compose up -d --no-build >"$BUILD_LOG.start" 2>&1; then
  echo '错误：新版本服务启动失败'
  cat "$BUILD_LOG.start"
  exit 21
fi

echo '等待服务健康检查'
health_ok=0
for _ in $(seq 1 90); do
  if curl -fsS http://127.0.0.1:18080/api/health; then
    echo
    health_ok=1
    break
  fi
  sleep 2
done
if [ "$health_ok" != 1 ]; then
  echo '错误：健康检查超时，显示服务日志'
  compose logs --tail=120 gateway api ai postgres redis
  exit 22
fi

echo '当前容器状态'
compose ps
echo '检查所有容器依赖与挂载权限'
for service in api gateway; do
  container="lpicto-${service}-1"
  service_health=''
  for _ in $(seq 1 30); do
    service_health="$(docker_cmd inspect -f '{{.State.Health.Status}}' "$container" 2>/dev/null || true)"
    [ "$service_health" = healthy ] && break
    [ "$service_health" = unhealthy ] && break
    sleep 1
  done
  if [ "$service_health" != healthy ]; then
    echo "错误：${service} 容器健康状态为 ${service_health:-unknown}"
    compose logs --tail=80 "$service"
    exit 24
  fi
done
compose exec -T api sh -lc 'test -r /Media && touch /cache/.api-write-check && rm /cache/.api-write-check'
compose exec -T ai sh -lc 'test -r /Media && test -r /cache && test -r /models/Qwen3VL-8B-Instruct-Q4_K_M.gguf'
compose exec -T postgres pg_isready -U media -d media
test "$(compose exec -T redis redis-cli ping | tr -d '\r')" = PONG
curl -fsS http://127.0.0.1:18080/api/settings/progress >/dev/null
curl -fsS http://127.0.0.1:18080/api/settings/libraries >/dev/null
echo '检查 AI 服务与模型'
ai_health_ok=0
for _ in $(seq 1 180); do
  if compose exec -T ai curl -fsS http://127.0.0.1:8090/health; then ai_health_ok=1; break; fi
  sleep 2
done
if [ "$ai_health_ok" != 1 ]; then
  echo '错误：AI 健康检查超时'
  compose logs --tail=160 ai
  exit 23
fi
echo
curl -fsS http://127.0.0.1:18080/api/ai/status
echo
echo '检查 CPU / GPU 视频编码'
compose exec -T api sh -lc \
  'test -r /dev/dri/renderD128 && test "$FFMPEG_HWACCEL" = auto && ffmpeg -hide_banner -encoders | grep -E "libx264|h264_vaapi" | head -n 6'

find "$HOME" -maxdepth 1 -type d -name 'lpicto.backup.*' -printf '%T@ %p\n' \
  | sort -nr \
  | tail -n +4 \
  | cut -d' ' -f2- \
  | xargs -r rm -rf

echo '远端部署完成'
'@

try {
    Write-Step "检查本地部署工具"
    foreach ($command in @('ssh', 'scp', 'sshpass', 'tar', 'curl.exe')) {
        if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
            Stop-Deploy "缺少命令：$command"
        }
    }
    Write-OK "ssh、scp、sshpass、tar 和 curl 均可用"

    Write-Step "检查远端 SSH 连接"
    $env:SSHPASS = $Password
    & sshpass -e ssh @sshOptions $remote "printf '远端连接成功\n'"
    if ($LASTEXITCODE -ne 0) {
        Stop-Deploy "无法连接到 ${remote}"
    }

    Write-Step "准备锁定版本 AI 模型"
    Prepare-LocalAIModels
    $externalAIToken = Get-ExternalAIToken
    Write-OK "模型文件已通过 SHA-256 校验"

    Write-Step "打包当前工作区"
    Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
    $excludes = @(
        './.git',
        './data',
        './cache',
        './artifacts',
        './frontend/node_modules',
        './frontend/dist',
        './frontend/tsconfig.tsbuildinfo',
        './.env',
        '*.db',
        '*.db-shm',
        '*.db-wal',
        './.codex',
        './.agents',
        './deploy.ps1',
        './lpicto-deploy.tgz'
    )
    $tarArguments = @('-czf', $archive) + ($excludes | ForEach-Object { '--exclude'; $_ }) + @('.')
    & tar @tarArguments
    if ($LASTEXITCODE -ne 0) { Stop-Deploy "创建部署归档失败" }
    Write-OK "归档创建完成，大小 $([math]::Round((Get-Item $archive).Length / 1KB)) KB"

    Write-Step "上传源码和远端执行器"
    & sshpass -e scp @sshOptions $archive "${remote}:${remoteArchive}"
    if ($LASTEXITCODE -ne 0) { Stop-Deploy "上传源码归档失败" }
    & sshpass -e ssh @sshOptions $remote "rm -rf ${remoteHome}/lpicto-ai-models-upload && mkdir -p ${remoteHome}/lpicto-ai-models-upload"
    if ($LASTEXITCODE -ne 0) { Stop-Deploy "创建远端 AI 模型上传目录失败" }
    foreach ($model in ($aiModelFiles | Where-Object { $_.Rel -notlike '*/*' })) {
        $remoteModelPath = "${remoteHome}/lpicto/data/app/ai-models/$($model.Rel)"
        & sshpass -e ssh @sshOptions $remote "test -f '$remoteModelPath' && echo '$($model.SHA)  $remoteModelPath' | sha256sum -c - >/dev/null && cp '$remoteModelPath' '${remoteHome}/lpicto-ai-models-upload/'"
        if ($LASTEXITCODE -eq 0) { continue }
        & sshpass -e scp @sshOptions (Join-Path $localAIModels $model.Rel) "${remote}:${remoteHome}/lpicto-ai-models-upload/"
        if ($LASTEXITCODE -ne 0) { Stop-Deploy "上传 AI 模型失败：$($model.Rel)" }
    }
    $remoteClipRoot = "${remoteHome}/lpicto/data/app/ai-models/chinese-clip"
    & sshpass -e ssh @sshOptions $remote "test -f '$remoteClipRoot/onnx/model.onnx' && echo 'd4e282affd5f09e196856cc63fbd0e77c576f598fdf6f6bb78ee61f1ef7cd770  $remoteClipRoot/onnx/model.onnx' | sha256sum -c - >/dev/null && cp -a '$remoteClipRoot' '${remoteHome}/lpicto-ai-models-upload/'"
    if ($LASTEXITCODE -ne 0) {
        & sshpass -e scp @sshOptions -r (Join-Path $localAIModels 'chinese-clip') "${remote}:${remoteHome}/lpicto-ai-models-upload/"
        if ($LASTEXITCODE -ne 0) { Stop-Deploy "上传 Chinese-CLIP 模型失败" }
    }
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($localRunner, ($remoteRunner -replace "`r", ""), $utf8NoBom)
    & sshpass -e scp @sshOptions $localRunner "${remote}:${remoteScript}"
    if ($LASTEXITCODE -ne 0) { Stop-Deploy "上传远端部署执行器失败" }
    Write-OK "文件上传完成"

    Write-Step "远端编译并重启服务"
    "$Password`n$externalAIToken" | & sshpass -e ssh @sshOptions $remote "bash ${remoteScript}"
    if ($LASTEXITCODE -ne 0) { Stop-Deploy "远端部署失败" }

    Write-Step "检查外部访问地址"
    $health = Invoke-RestMethod -Uri "http://${HostIP}:${Port}/api/health" -TimeoutSec 10
    if ($health.status -ne 'ok') { Stop-Deploy "健康接口返回异常" }
    Write-OK "健康检查通过"
    Write-Host "`n部署完成：http://${HostIP}:${Port}" -ForegroundColor Green
}
catch {
    Write-Host "`n部署失败：$($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
finally {
    Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $localRunner -Force -ErrorAction SilentlyContinue
    Remove-Item Env:SSHPASS -ErrorAction SilentlyContinue
}
