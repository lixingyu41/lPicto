$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$tokenPath = Join-Path $env:LOCALAPPDATA "lPicto\external-ai-token"
$env:LPICTO_EXTERNAL_AI_TOKEN = if (Test-Path -LiteralPath $tokenPath) { ([IO.File]::ReadAllText($tokenPath)).Trim() } else { "missing" }
$env:LPICTO_EXTERNAL_AI_MODELS = Join-Path $env:LOCALAPPDATA "lPicto\ai-models"
try {
    & docker.exe compose -p lpicto-external-ai -f docker-compose.external-ai.yml down
    if ($LASTEXITCODE -ne 0) { throw "停止 lPicto 外部 AI 失败" }
} finally {
    Remove-Item Env:LPICTO_EXTERNAL_AI_TOKEN,Env:LPICTO_EXTERNAL_AI_MODELS -ErrorAction SilentlyContinue
}
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if ($isAdmin) {
    Get-NetFirewallRule -DisplayName "lPicto External AI *" -ErrorAction SilentlyContinue | Remove-NetFirewallRule
}
Write-Host "lPicto 外部 AI 已停止，模型文件仍保留。"
