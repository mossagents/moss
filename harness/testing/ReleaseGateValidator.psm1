param()

<#
.SYNOPSIS
Runs the real smoke + replay release gate probe.

.DESCRIPTION
This module shells out to the Go-based release gate probe so PowerShell
workflows can keep driving CI while the actual metric collection and gate
validation stay in the Go runtime.
#>

function Invoke-ReleaseGateProbe {
    param(
        [string]$WorkspaceRoot = ".",
        [ValidateSet("prod", "staging", "dev")]
        [string]$Environment = "prod",
        [string]$GoPath = "go"
    )

    $jsonPath = Join-Path ([System.IO.Path]::GetTempPath()) ("moss-release-gate-" + [guid]::NewGuid().ToString() + ".json")
    Push-Location $WorkspaceRoot
    try {
        $output = & $GoPath run ./harness/testing/cmd/releasegateprobe -env $Environment -json-output $jsonPath 2>&1
        $exitCode = $LASTEXITCODE
        $report = $null
        if (Test-Path $jsonPath) {
            $raw = Get-Content -Path $jsonPath -Raw
            if (-not [string]::IsNullOrWhiteSpace($raw)) {
                $report = $raw | ConvertFrom-Json -Depth 16
            }
        }
        return @{
            ExitCode = $exitCode
            Output   = (($output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine)
            Report   = $report
        }
    }
    finally {
        Pop-Location
        if (Test-Path $jsonPath) {
            Remove-Item $jsonPath -Force -ErrorAction SilentlyContinue
        }
    }
}

function Test-ReleaseGateProbeResult {
    param(
        [hashtable]$Invocation
    )

    return $Invocation -ne $null -and $Invocation.ExitCode -eq 0
}

Export-ModuleMember -Function @(
    'Invoke-ReleaseGateProbe',
    'Test-ReleaseGateProbeResult'
)

