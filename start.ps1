[CmdletBinding()]
param([switch]$Demo)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    if (!(Test-Path -LiteralPath '.env') -and !$env:DATABASE_URL) {
        throw '请先复制 .env.example 为 .env 并填写数据库与账号配置。'
    }
    Push-Location web
    try {
        if (!(Test-Path -LiteralPath 'node_modules')) {
            npm.cmd ci
            if ($LASTEXITCODE) { throw '前端依赖安装失败' }
        }
        npm.cmd run build
        if ($LASTEXITCODE) { throw '前端构建失败' }
    } finally { Pop-Location }
    if ($Demo) { go run ./cmd/viid -demo } else { go run ./cmd/viid }
    if ($LASTEXITCODE) { throw 'VIID Hub 服务已异常退出' }
} finally { Pop-Location }
