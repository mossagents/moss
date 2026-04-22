param()

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$examplesRoot = Join-Path $repoRoot "examples"
if (-not (Test-Path $examplesRoot)) {
  throw "examples directory not found"
}

Push-Location $repoRoot
try {
  $modules = Get-ChildItem -Path $examplesRoot -Recurse -Filter go.mod | Sort-Object FullName
  if ($modules.Count -eq 0) {
    throw "no example go.mod files found"
  }

  foreach ($module in $modules) {
    $dir = Split-Path $module.FullName -Parent
    Write-Host "==> $dir"
    Push-Location $dir
    try {
      go test ./...
      if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
      }
      go build .
      if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
      }
    }
    finally {
      Pop-Location
    }
  }

  Write-Host "Example module validation passed."
}
finally {
  Pop-Location
}
