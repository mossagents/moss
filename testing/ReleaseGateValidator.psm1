param()

<#
.SYNOPSIS
Collects and validates release gates based on normalized metrics.

.DESCRIPTION
This script collects metrics from a test run and validates them against
production release gate thresholds to prevent non-compliant releases.
#>

function Get-NormalizedMetrics {
    param(
        [string]$GoPath = "go"
    )

    try {
        # Run a quick test to trigger metric collection
        Write-Verbose "Collecting normalized metrics..."
        $output = & $GoPath test ./kernel/observe/... -run TestMetricsAccumulator -v 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Could not collect metrics from test suite"
            return $null
        }
        return $output
    }
    catch {
        Write-Warning "Error collecting metrics: $_"
        return $null
    }
}

function New-ReleaseGate {
    param(
        [string]$Name,
        [string]$Description,
        [double]$Threshold,
        [string]$MetricKey,
        [string]$Operator,
        [bool]$Enabled = $true
    )

    return @{
        Name        = $Name
        Description = $Description
        Threshold   = $Threshold
        MetricKey   = $MetricKey
        Operator    = $Operator
        Enabled     = $Enabled
    }
}

function Get-DefaultReleaseGates {
    param(
        [ValidateSet("prod", "staging", "dev")]
        [string]$Environment = "prod"
    )

    $gates = @()

    if ($Environment -eq "prod") {
        $gates += New-ReleaseGate -Name "success_rate" `
            -Description "Run success rate (completed / total)" `
            -Threshold 0.95 `
            -MetricKey "success.rate" `
            -Operator "gte"

        $gates += New-ReleaseGate -Name "llm_latency_avg" `
            -Description "Average LLM latency (ms)" `
            -Threshold 10000 `
            -MetricKey "latency.llm_avg_ms" `
            -Operator "lte"

        $gates += New-ReleaseGate -Name "tool_latency_avg" `
            -Description "Average tool latency (ms)" `
            -Threshold 5000 `
            -MetricKey "latency.tool_avg_ms" `
            -Operator "lte"

        $gates += New-ReleaseGate -Name "tool_error_rate" `
            -Description "Tool error rate (errors / total calls)" `
            -Threshold 0.05 `
            -MetricKey "tool_error.rate" `
            -Operator "lte"
    }
    elseif ($Environment -eq "staging") {
        # Relaxed thresholds for staging
        $gates += New-ReleaseGate -Name "success_rate" `
            -Description "Run success rate (completed / total)" `
            -Threshold 0.90 `
            -MetricKey "success.rate" `
            -Operator "gte"

        $gates += New-ReleaseGate -Name "llm_latency_avg" `
            -Description "Average LLM latency (ms)" `
            -Threshold 15000 `
            -MetricKey "latency.llm_avg_ms" `
            -Operator "lte"

        $gates += New-ReleaseGate -Name "tool_latency_avg" `
            -Description "Average tool latency (ms)" `
            -Threshold 8000 `
            -MetricKey "latency.tool_avg_ms" `
            -Operator "lte"

        $gates += New-ReleaseGate -Name "tool_error_rate" `
            -Description "Tool error rate (errors / total calls)" `
            -Threshold 0.10 `
            -MetricKey "tool_error.rate" `
            -Operator "lte"
    }
    else {
        # Dev mode - only check success rate
        $gates += New-ReleaseGate -Name "success_rate" `
            -Description "Run success rate (completed / total)" `
            -Threshold 0.80 `
            -MetricKey "success.rate" `
            -Operator "gte" `
            -Enabled $true
    }

    return $gates
}

function Compare-MetricValue {
    param(
        [double]$Value,
        [double]$Threshold,
        [string]$Operator
    )

    switch ($Operator) {
        "gte" { return $Value -ge $Threshold }
        "lte" { return $Value -le $Threshold }
        "eq"  { return $Value -eq $Threshold }
        default { return $false }
    }
}

function Test-ReleaseGate {
    param(
        [hashtable]$Gate,
        [double]$Value
    )

    if (-not $Gate.Enabled) {
        return @{
            Gate   = $Gate
            Value  = $Value
            Passed = $true
            Reason = "Gate disabled (skipped)"
        }
    }

    $passed = Compare-MetricValue -Value $Value -Threshold $Gate.Threshold -Operator $Gate.Operator
    $reason = "$($Gate.MetricKey) $($Gate.Operator) $($Gate.Threshold) (value: $Value)"
    if (-not $passed) {
        $reason += " [FAILED]"
    }

    return @{
        Gate   = $Gate
        Value  = $Value
        Passed = $passed
        Reason = $reason
    }
}

function Format-GateReport {
    param(
        [array]$Results,
        [string]$Environment,
        [int]$FailCount,
        [bool]$AllPassed
    )

    $report = @()
    $report += "=== Release Gate Validation Report ==="
    $report += "Environment: $Environment"
    $report += "Overall: $(if ($AllPassed) { '✓ PASSED' } else { '✗ FAILED' }) ($FailCount gate(s) failed)"
    $report += ""
    $report += "Gate Results:"

    foreach ($result in $Results) {
        $status = if ($result.Passed) { "✓" } else { "✗" }
        $report += "  $status $($result.Gate.Name): $($result.Reason)"
    }

    return $report -join [Environment]::NewLine
}

Export-ModuleMember -Function @(
    'Get-NormalizedMetrics',
    'New-ReleaseGate',
    'Get-DefaultReleaseGates',
    'Compare-MetricValue',
    'Test-ReleaseGate',
    'Format-GateReport'
)

