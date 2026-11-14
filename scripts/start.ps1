param([ValidateSet('', 'demo', 'gigachat')][string]$Mode = '')
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
$env:COMPOSE_BAKE = 'false'
$env:DOCKER_BUILDKIT = '0'
if ($Mode) { $env:LLM_MODE = $Mode }
docker compose up --build -d --wait
if ($LASTEXITCODE -ne 0) { throw 'Container build/start failed; inspect Docker Engine and compose logs.' }
Write-Output 'Onbo is available at http://localhost:8080'
