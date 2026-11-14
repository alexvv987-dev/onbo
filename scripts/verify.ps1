param([switch]$SkipIntegration)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Force reports | Out-Null
if (-not $SkipIntegration) {
    docker compose up -d --wait postgres redis
    if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL/Redis startup failed' }
    $env:ONBO_TEST_DATABASE_URL = 'postgres://onbo:onbo-demo-local@127.0.0.1:55432/onbo?sslmode=disable'
    $env:ONBO_TEST_REDIS_ADDR = '127.0.0.1:16379'
}
$unformattedGo = @(gofmt -l cmd internal)
if ($LASTEXITCODE -ne 0 -or $unformattedGo.Count -ne 0) { throw 'Go formatting check failed' }
go vet ./...
if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
go run scripts/check-go-style.go
if ($LASTEXITCODE -ne 0) { throw 'Go naming/constant layout check failed' }
go test ./... -count=1 '-coverpkg=./...' '-coverprofile=reports/go.cover'
if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
Push-Location web
try {
    npm run format:check
    if ($LASTEXITCODE -ne 0) { throw 'Frontend formatting failed' }
    npm run typecheck
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript failed' }
    npm run test:coverage
    if ($LASTEXITCODE -ne 0) { throw 'UI tests failed' }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
} finally { Pop-Location }
node scripts/coverage.mjs
if ($LASTEXITCODE -ne 0) { throw 'Coverage threshold failed' }
npm run --silent openspec -- validate --all --strict
if ($LASTEXITCODE -ne 0) { throw 'SDD validation failed' }
docker compose config --quiet
if ($LASTEXITCODE -ne 0) { throw 'Compose validation failed' }
git diff --check
if ($LASTEXITCODE -ne 0) { throw 'Whitespace validation failed' }
