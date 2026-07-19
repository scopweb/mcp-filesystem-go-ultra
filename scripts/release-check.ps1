#Requires -Version 7.0
<#
.SYNOPSIS
  Release gate for mcp-filesystem-go-ultra (Fase 2 del plan de endurecimiento).

.DESCRIPTION
  Runs every check that must be green before tagging a release. Mirrors
  .github/workflows/ci.yml — if this passes locally, CI should pass.

  Gates:
    1. go build ./...
    2. go vet ./...
    3. go test ./... (full suite)
    4. go test -tags e2e ./tests/e2e/ (strict-client smoke battery)
    5. govulncheck ./...
    6. go mod tidy (no diff allowed)
    7. gofmt on files changed vs origin/main
    8. Version coherence: CHANGELOG top entry vs serverVersion (warning only)

  Weekly cadence: cut a release from a green main — do not release from
  ungated commits. This script IS the gate; CI enforces it on every push/PR.

.EXAMPLE
  pwsh scripts/release-check.ps1
  pwsh scripts/release-check.ps1 -SkipE2E -SkipVuln   # fast local iteration
#>
param(
  [switch]$SkipE2E,
  [switch]$SkipVuln
)

Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
$script:failed = [System.Collections.Generic.List[string]]::new()
$script:warned = [System.Collections.Generic.List[string]]::new()

function Invoke-Gate([string]$Name, [scriptblock]$Block) {
  Write-Host "`n==> $Name" -ForegroundColor Cyan
  $sw = [System.Diagnostics.Stopwatch]::StartNew()
  & $Block | Out-Default
  $code = $LASTEXITCODE
  $sw.Stop()
  if ($code -ne 0) {
    $script:failed.Add($Name)
    Write-Host "FAIL  $Name (exit $code, $([int]$sw.Elapsed.TotalSeconds)s)" -ForegroundColor Red
  } else {
    Write-Host "OK    $Name ($([int]$sw.Elapsed.TotalSeconds)s)" -ForegroundColor Green
  }
}

Invoke-Gate "build" { go build ./... }
Invoke-Gate "vet" { go vet ./... }
Invoke-Gate "test (full suite)" { go test ./... -count=1 }
if (-not $SkipE2E) {
  Invoke-Gate "test e2e (strict client smoke)" { go test -tags e2e ./tests/e2e/ -count=1 }
}
if (-not $SkipVuln) {
  if (-not (Get-Command govulncheck -ErrorAction SilentlyContinue)) {
    Write-Host "govulncheck not found — installing..." -ForegroundColor Yellow
    go install golang.org/x/vuln/cmd/govulncheck@latest
  }
  Invoke-Gate "govulncheck" { govulncheck ./... }
}

Invoke-Gate "go.mod tidy" {
  go mod tidy
  git diff --exit-code go.mod go.sum | Out-Null
}

# gofmt only on files changed vs origin/main (repo has pre-existing drift;
# CI enforces the same rule). Skip gracefully when origin/main is unavailable.
Invoke-Gate "gofmt (changed files)" {
  $base = "origin/main"
  git rev-parse --verify $base 2>$null | Out-Null
  if ($LASTEXITCODE -ne 0) {
    Write-Host "origin/main not available locally — skipping gofmt gate" -ForegroundColor Yellow
    $script:warned.Add("gofmt (no base ref)")
    $global:LASTEXITCODE = 0
    return
  }
  $changed = git diff --name-only --diff-filter=ACM "$base...HEAD" -- "*.go"
  $bad = @($changed | Where-Object { $_ } | ForEach-Object { gofmt -l $_ } | Where-Object { $_ })
  if ($bad.Count -gt 0) {
    Write-Host "Not gofmt'ed:" -ForegroundColor Red
    $bad | ForEach-Object { Write-Host "  $_" }
    $global:LASTEXITCODE = 1
  } else {
    $global:LASTEXITCODE = 0
  }
}

# Version coherence: informational. During development the CHANGELOG top entry
# is "[Unreleased / X.Y.Z]" while serverVersion still shows the previous
# release — that is expected. Warn when they match NEITHER pattern sanely.
Write-Host "`n==> version coherence (info)" -ForegroundColor Cyan
$serverVersion = (Select-String -Path main.go -Pattern 'const serverVersion = "([^"]+)"').Matches.Groups[1].Value
$changelogTop = (Select-String -Path CHANGELOG.md -Pattern '^## \[(?:Unreleased / )?([0-9]+\.[0-9]+\.[0-9]+)\]' | Select-Object -First 1).Matches.Groups[1].Value
Write-Host "  serverVersion = $serverVersion | CHANGELOG top = $changelogTop"
if ($changelogTop -ne $serverVersion -and [version]$changelogTop -le [version]$serverVersion) {
  $script:warned.Add("version: CHANGELOG top ($changelogTop) <= serverVersion ($serverVersion) — missing entry?")
  Write-Host "  WARN: CHANGELOG top entry is not ahead of serverVersion — did you forget the entry?" -ForegroundColor Yellow
}

Write-Host "`n========================================" -ForegroundColor Cyan
if ($script:failed.Count -eq 0) {
  Write-Host "RELEASE GATE: ALL GREEN" -ForegroundColor Green
  if ($script:warned.Count -gt 0) {
    Write-Host "Warnings:" -ForegroundColor Yellow
    $script:warned | ForEach-Object { Write-Host "  - $_" -ForegroundColor Yellow }
  }
  exit 0
}
Write-Host "RELEASE GATE: FAILED ($($script:failed.Count) gate(s))" -ForegroundColor Red
$script:failed | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
exit 1
