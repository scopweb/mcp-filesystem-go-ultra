param(
    [string]$Exe
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root

$manifestPath = Join-Path $root "bundle\manifest.json"
$manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding utf8 | ConvertFrom-Json
$version = $manifest.version
if (-not $version) { throw "bundle/manifest.json has no version" }

$commit = "dev"
$git = Get-Command git -ErrorAction SilentlyContinue
if ($git) {
    $commit = (& git rev-parse --short HEAD).Trim()
}
$date = Get-Date -Format "yyyy-MM-dd"
$ldflags = "-s -w -X github.com/mcp/filesystem-ultra/internal/mcpserver.BuildCommit=$commit -X github.com/mcp/filesystem-ultra/internal/mcpserver.BuildDate=$date"

$stage = Join-Path $root "dist\mcpb-stage"
$serverDir = Join-Path $stage "server"
if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Path $serverDir | Out-Null

$exe = Join-Path $serverDir "filesystem-ultra.exe"
if ($Exe) {
    if (-not (Test-Path -LiteralPath $Exe)) { throw "missing built server: $Exe" }
    Copy-Item -LiteralPath $Exe -Destination $exe
} else {
    & go build -ldflags="$ldflags" -trimpath -o $exe .\cmd\filesystem-ultra
    if ($LASTEXITCODE -ne 0) { throw "go build failed: $LASTEXITCODE" }
}

Copy-Item -LiteralPath $manifestPath -Destination (Join-Path $stage "manifest.json")

$outDir = Join-Path $root "dist"
New-Item -ItemType Directory -Path $outDir -Force | Out-Null
$out = Join-Path $outDir "filesystem-ultra-$version-win32.mcpb"
if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Force }

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [System.IO.Compression.ZipFile]::Open($out, [System.IO.Compression.ZipArchiveMode]::Create)
try {
    $entries = @(
        @{ Name = "manifest.json"; Path = (Join-Path $stage "manifest.json") },
        @{ Name = "server/filesystem-ultra.exe"; Path = $exe }
    )
    foreach ($item in $entries) {
        $entry = $zip.CreateEntry($item.Name, [System.IO.Compression.CompressionLevel]::Optimal)
        $input = [System.IO.File]::OpenRead($item.Path)
        try {
            $target = $entry.Open()
            try { $input.CopyTo($target) } finally { $target.Dispose() }
        } finally { $input.Dispose() }
    }
} finally {
    $zip.Dispose()
}

Remove-Item -LiteralPath $stage -Recurse -Force
Write-Output $out
