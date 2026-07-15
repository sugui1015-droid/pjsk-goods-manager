# =====================================================================
# 示例文件，禁止直接用于生产环境。
# EXAMPLE ONLY — DO NOT USE IN PRODUCTION AS-IS.
#
# 用途：以受控方式启动后端 exe 的示例包装脚本。
#   - 默认只做配置检查（-CheckOnly）：验证 exe 与环境文件存在，不启动进程。
#   - 显式加 -Run 时才真正启动后端（前台进程，供人工/包装器托管）。
# 本脚本【不会】安装服务、【不会】提升管理员权限、【不会】下载 WinSW/NSSM/Caddy、
# 【不会】修改系统级环境变量，也【不会】输出任何环境变量的值。
#
# 密钥策略：真实环境变量放在受 ACL 保护的 $EnvFile（默认 D:\pjsk\secrets\backend.env）。
# 本脚本仅确认该文件存在，不读取、不解析、不回显其内容——后端自身用 godotenv 从工作目录读取。
# =====================================================================
[CmdletBinding()]
param(
    [string]$ExePath = 'D:\pjsk\backend\bin\pjsk-backend.exe',
    [string]$WorkingDirectory = 'D:\pjsk\secrets',
    [string]$EnvFile = 'D:\pjsk\secrets\backend.env',
    [switch]$Run
)

$ErrorActionPreference = 'Stop'

function Fail([string]$Message) {
    Write-Error $Message
    exit 1
}

# --- validate the backend executable exists ---
if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    Fail "Backend executable not found: $ExePath. Build it with: go build -trimpath -o .\bin\pjsk-backend.exe . (from D:\pjsk\backend)"
}

# --- validate the working directory exists ---
if (-not (Test-Path -LiteralPath $WorkingDirectory -PathType Container)) {
    Fail "Working directory not found: $WorkingDirectory"
}

# --- validate the environment file exists (content is NEVER read or printed) ---
if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
    Fail "Environment file not found: $EnvFile. Create it out of Git with restricted ACLs (see docs/windows-service-deployment.md)."
}

Write-Output "Backend executable : $ExePath"
Write-Output "Working directory  : $WorkingDirectory"
Write-Output "Environment file   : present (content not read)"

if (-not $Run) {
    Write-Output "Check only (no -Run): configuration looks valid; not starting the backend."
    exit 0
}

# --- start the backend in the foreground; caller/service wrapper owns the lifecycle ---
Write-Output "Starting backend..."
Push-Location -LiteralPath $WorkingDirectory
try {
    & $ExePath
    $exitCode = $LASTEXITCODE
}
finally {
    Pop-Location
}
Write-Output "Backend exited with code $exitCode."
exit $exitCode
