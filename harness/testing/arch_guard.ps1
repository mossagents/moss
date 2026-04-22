param(
    [ValidateSet("prod", "staging", "dev")]
    [string]$Environment = "prod",

    [switch]$SkipGates,

    [string]$OverrideReason = "",

    [switch]$Help
)

$ErrorActionPreference = "Stop"

if ($Help) {
    Write-Host @"
Usage: arch_guard.ps1 [options]

Options:
  -Environment <prod|staging|dev>   Release environment (default: prod)
  -SkipGates                         Skip release gate validation
  -OverrideReason <string>          Override prod gate failures with incident reason
  -Help                             Show this help message

Examples:
  .\arch_guard.ps1                                    # Run prod checks
  .\arch_guard.ps1 -Environment staging               # Run staging checks
  .\arch_guard.ps1 -OverrideReason "incident-001"    # Override with reason
"@
    exit 0
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$workspaceFile = Join-Path $repoRoot "go.work"
if (-not (Test-Path $workspaceFile)) {
  throw "go.work not found at repository root"
}
$gateModule = Join-Path $PSScriptRoot "ReleaseGateValidator.psm1"
Import-Module $gateModule -Force

Push-Location $repoRoot
try {
  Write-Host "=== Moss Release Guard ===" -ForegroundColor Cyan
  Write-Host "Environment: $Environment"
  Write-Host ""

  # ============================================================================
  # 1. ARCHITECTURE COMPLIANCE CHECK
  # ============================================================================
  Write-Host "1. Architecture Compliance Check" -ForegroundColor Yellow
  $lines = go list -f '{{.ImportPath}}|{{.Imports}}' ./... 2>$null
  $violations = @()

  foreach ($line in $lines) {
    if ([string]::IsNullOrWhiteSpace($line)) { continue }
    $parts = $line.Split("|", 2)
    $pkg = $parts[0]
    $imports = @()
    if ($parts.Length -gt 1 -and -not [string]::IsNullOrWhiteSpace($parts[1])) {
      # Parse the imports array format [pkg1 pkg2 ...]
      $importsStr = $parts[1].Trim('[]')
      if ($importsStr -ne "") {
        $imports = $importsStr.Split(" ")
      }
    }

    if ($pkg -match '/cmd($|/)') { continue }

    foreach ($imp in $imports) {
      if ([string]::IsNullOrWhiteSpace($imp)) { continue }
      if ($imp -match '/cmd($|/)') {
        $violations += "$pkg imports $imp"
      }
    }
  }

  if ($violations.Count -gt 0) {
    Write-Host "  ✗ FAILED: non-cmd packages importing cmd detected:" -ForegroundColor Red
    $violations | ForEach-Object { Write-Host "    - $_" }
    exit 1
  }

  Write-Host "  ✓ PASSED: All architecture rules compliant" -ForegroundColor Green

  # ============================================================================
  # 2. RELEASE GATE VALIDATION (if enabled)
  # ============================================================================
  if (-not $SkipGates) {
      Write-Host ""
      Write-Host "2. Release Gate Validation" -ForegroundColor Yellow
      $probe = Invoke-ReleaseGateProbe -WorkspaceRoot $repoRoot -Environment $Environment
      if ($probe.Output) {
          Write-Host $probe.Output
      }

      if (-not (Test-ReleaseGateProbeResult $probe)) {
          if ($OverrideReason) {
              $trimmedOverrideReason = $OverrideReason.Trim()
              if ($Environment -ne "prod") {
                  Write-Host "  ✗ FAILED: overrides are only allowed for prod release gates" -ForegroundColor Red
                  exit 1
              }
              if ($trimmedOverrideReason.Length -lt 5) {
                  Write-Host "  ✗ FAILED: override reason must be at least 5 characters" -ForegroundColor Red
                  exit 1
              }

              Write-Host "  ⚠ OVERRIDE ACTIVATED: $trimmedOverrideReason" -ForegroundColor Yellow
              Write-Host "    > Recording override in audit trail" -ForegroundColor Cyan

              $overrideLog = Join-Path $repoRoot "docs/release-overrides.log"
              $logDir = Split-Path $overrideLog
              if (-not (Test-Path $logDir)) {
                  New-Item -ItemType Directory -Path $logDir -Force | Out-Null
              }

              $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
              $summary = ""
              if ($probe.Report -and $probe.Report.gate_status) {
                  $summary = " | fail_count=$($probe.Report.gate_status.fail_count)"
              }
              $logEntry = "$timestamp | $Environment | $trimmedOverrideReason | $(whoami)$summary"
              Add-Content -Path $overrideLog -Value $logEntry

              Write-Host "    > Override recorded at $overrideLog" -ForegroundColor Cyan
          }
          else {
              Write-Host "  ✗ FAILED: release gates did not pass" -ForegroundColor Red
              exit 1
          }
      }
      else {
          Write-Host "  ✓ PASSED: release gate probe satisfied all thresholds" -ForegroundColor Green
      }
  }

  Write-Host ""
  Write-Host "=== All Release Guards Passed ===" -ForegroundColor Green
  exit 0
}
finally {
  Pop-Location
}
