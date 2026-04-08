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
  -OverrideReason <string>          Override gate failures with incident reason
  -Help                             Show this help message

Examples:
  .\arch_guard.ps1                                    # Run prod checks
  .\arch_guard.ps1 -Environment staging               # Run staging checks
  .\arch_guard.ps1 -OverrideReason "incident-001"    # Override with reason
"@
    exit 0
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$modPath = Join-Path $repoRoot "go.mod"
if (-not (Test-Path $modPath)) {
  throw "go.mod not found at repository root"
}

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

    # For now, gates are informational since we don't have runtime metrics collection yet
    # This will be populated by the CI/CD pipeline or integration with observer
    Write-Host "  ℹ Gates defined (runtime metrics validation to be integrated)" -ForegroundColor Cyan
    Write-Host "    - success_rate: >= 0.95 (prod) / >= 0.90 (staging)"
    Write-Host "    - llm_latency_avg: <= 10000ms (prod) / <= 15000ms (staging)"
    Write-Host "    - tool_latency_avg: <= 5000ms (prod) / <= 8000ms (staging)"
    Write-Host "    - tool_error_rate: <= 0.05 (prod) / <= 0.10 (staging)"

    if ($OverrideReason) {
        Write-Host "  ⚠ OVERRIDE ACTIVATED: $OverrideReason" -ForegroundColor Yellow
        Write-Host "    > Recording override in audit trail" -ForegroundColor Cyan

        $overrideLog = Join-Path $repoRoot "docs/release-overrides.log"
        $logDir = Split-Path $overrideLog
        if (-not (Test-Path $logDir)) {
            New-Item -ItemType Directory -Path $logDir -Force | Out-Null
        }

        $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
        $logEntry = "$timestamp | $Environment | $OverrideReason | $(whoami)"
        Add-Content -Path $overrideLog -Value $logEntry

        Write-Host "    > Override recorded at $overrideLog" -ForegroundColor Cyan
    }
}

Write-Host ""
Write-Host "=== All Release Guards Passed ===" -ForegroundColor Green
exit 0
