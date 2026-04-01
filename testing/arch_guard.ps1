param()

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$modPath = Join-Path $repoRoot "go.mod"
if (-not (Test-Path $modPath)) {
  throw "go.mod not found at repository root"
}

$lines = go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./... 2>$null
$violations = @()

foreach ($line in $lines) {
  if ([string]::IsNullOrWhiteSpace($line)) { continue }
  $parts = $line.Split("|", 2)
  $pkg = $parts[0]
  $imports = @()
  if ($parts.Length -gt 1 -and -not [string]::IsNullOrWhiteSpace($parts[1])) {
    $imports = $parts[1].Split(",")
  }

  if ($pkg -match '/cmd($|/)') { continue }

  foreach ($imp in $imports) {
    if ($imp -match '/cmd($|/)') {
      $violations += "$pkg imports $imp"
    }
  }
}

if ($violations.Count -gt 0) {
  Write-Host "Architecture guard failed: non-cmd packages importing cmd detected:"
  $violations | ForEach-Object { Write-Host " - $_" }
  exit 1
}

Write-Host "Architecture guard passed."
