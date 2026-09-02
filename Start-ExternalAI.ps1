param(
    [int]$Port = 18090,
    [string]$BindAddress = "",
    [string]$Models = (Join-Path $env:LOCALAPPDATA "lPicto\ai-models"),
    [string]$UbuntuHost = "192.168.2.97"
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
if ($Port -lt 1 -or $Port -gt 65535) { throw "端口必须为 1 到 65535" }
if ([string]::IsNullOrWhiteSpace($BindAddress)) {
    $BindAddress = (Find-NetRoute -RemoteIPAddress $UbuntuHost | Select-Object -First 1 -ExpandProperty IPAddress)
}
$parsedAddress = $null
if (-not [Net.IPAddress]::TryParse($BindAddress, [ref]$parsedAddress)) { throw "绑定地址不是有效 IP：$BindAddress" }
$required = @(
    @{ Rel = "Qwen3VL-8B-Instruct-Q4_K_M.gguf"; Size = 5027784800L; SHA = "67d1659bfe71b89d50b45a4ad1a9e5b997e5bb16ce5da66a6a6167abd569e9e2" },
    @{ Rel = "mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf"; Size = 752289728L; SHA = "c6ba85508d82f42590e6eb77d5340369ab6fecf107a7561d809523d8aa5f3bfd" },
    @{ Rel = "chinese-clip\onnx\model.onnx"; Size = 753665706L; SHA = "d4e282affd5f09e196856cc63fbd0e77c576f598fdf6f6bb78ee61f1ef7cd770" }
)
foreach ($model in $required) {
    $path = Join-Path $Models $model.Rel
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "缺少 AI 模型：$path" }
    if ((Get-Item -LiteralPath $path).Length -ne $model.Size -or (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $model.SHA) {
        throw "AI 模型校验失败：$path"
    }
}
if (-not (Get-Command docker.exe -ErrorAction SilentlyContinue)) { throw "未安装 Docker Desktop" }
& docker.exe info *> $null
if ($LASTEXITCODE -ne 0) {
    $desktop = Join-Path $env:ProgramFiles "Docker\Docker\Docker Desktop.exe"
    if (-not (Test-Path -LiteralPath $desktop)) { throw "Docker Desktop 未安装或路径不可用" }
    Start-Process -FilePath $desktop -WindowStyle Hidden
    $deadline = [DateTime]::UtcNow.AddMinutes(3)
    do {
        Start-Sleep -Seconds 3
        & docker.exe info *> $null
        $ready = $LASTEXITCODE -eq 0
    } until ($ready -or [DateTime]::UtcNow -ge $deadline)
    if (-not $ready) { throw "Docker Desktop 在 3 分钟内未启动" }
}

$tokenDirectory = Join-Path $env:LOCALAPPDATA "lPicto"
$tokenPath = Join-Path $tokenDirectory "external-ai-token"
New-Item -ItemType Directory -Force $tokenDirectory | Out-Null
if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
    $tokenBytes = New-Object byte[] 32
    [Security.Cryptography.RandomNumberGenerator]::Fill($tokenBytes)
    [IO.File]::WriteAllText($tokenPath, [Convert]::ToHexString($tokenBytes).ToLowerInvariant())
}
$externalAIToken = ([IO.File]::ReadAllText($tokenPath)).Trim()
if ($externalAIToken -notmatch '^[0-9a-f]{64}$') { throw "外部 AI 认证密钥无效：$tokenPath" }

$env:LPICTO_EXTERNAL_AI_MODELS = (Resolve-Path -LiteralPath $Models).Path
$env:LPICTO_EXTERNAL_AI_PORT = [string]$Port
$env:LPICTO_EXTERNAL_AI_BIND = $BindAddress
$env:LPICTO_EXTERNAL_AI_TOKEN = $externalAIToken
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
$ruleName = "lPicto External AI $Port"
try {
    if ($isAdmin) {
        Remove-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue
        New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Action Allow -Protocol TCP -LocalAddress $BindAddress -LocalPort $Port -RemoteAddress $UbuntuHost | Out-Null
    }
    & docker.exe compose -p lpicto-external-ai -f docker-compose.external-ai.yml up -d --build
    if ($LASTEXITCODE -ne 0) { throw "外部 AI 容器启动失败" }
    $healthHost = if ($BindAddress -eq "0.0.0.0") { "127.0.0.1" } else { $BindAddress }
    $deadline = [DateTime]::UtcNow.AddMinutes(3)
    do {
        Start-Sleep -Seconds 2
        try {
            $health = Invoke-RestMethod -Uri "http://${healthHost}:$Port/health" -TimeoutSec 5
            $ready = $health.status -eq "ok" -and $health.onnxProviders -contains "CUDAExecutionProvider" -and $health.gpuActive -eq $true
        } catch { $ready = $false }
    } until ($ready -or [DateTime]::UtcNow -ge $deadline)
    if (-not $ready) { throw "外部 AI 在 3 分钟内未通过 CUDA 健康检查" }
} catch {
    & docker.exe compose -p lpicto-external-ai -f docker-compose.external-ai.yml down *> $null
    throw
} finally {
    Remove-Item Env:LPICTO_EXTERNAL_AI_MODELS,Env:LPICTO_EXTERNAL_AI_PORT,Env:LPICTO_EXTERNAL_AI_BIND,Env:LPICTO_EXTERNAL_AI_TOKEN -ErrorAction SilentlyContinue
}

Write-Host "lPicto 外部 AI 已启动，端口 $Port，llama.cpp CUDA 已启用，ONNX $($health.onnxProviders -join ', ')"
Write-Host "在 lPicto 设置中填写：$BindAddress / $Port"
if (-not $isAdmin) { Write-Warning "当前未创建 Windows 防火墙白名单；接口已有 256 位认证密钥保护。" }
