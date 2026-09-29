# Isolated agents: same model/data/workers, only JEV differs.
# Requires billing/MySQL/Redis. Does not edit .env or restart the main agent.
[CmdletBinding()]
param(
    [string]$Key = 'sk-demo-8f3a2b1c9d4e5f60',
    [int]$Sample = 0,
    [int]$Workers = 4,
    [int]$BaselinePort = 19105,
    [int]$ExperimentPort = 19106,
    [string]$Out = ('eval/reports/ab-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
)
$ErrorActionPreference = 'Stop'
$rootPath = Split-Path -Parent $PSScriptRoot
Push-Location $rootPath
$processes = @()
$savedPort = $env:AGENT_PORT
$savedJev = $env:JEV_API_KEY
try {
    . "$PSScriptRoot/load-env.ps1"
    if (!$env:AGENT_LLM_API_KEY -or !$env:JEV_API_KEY) { throw 'AGENT_LLM_API_KEY and JEV_API_KEY are required.' }
    if ($Workers -lt 1 -or $Sample -lt 0 -or $BaselinePort -eq $ExperimentPort) { throw 'Invalid workers, sample or ports.' }
    foreach ($port in @($BaselinePort, $ExperimentPort)) {
        if (Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue) { throw "Port $port is already in use; choose another port." }
    }
    $model = if ($env:AGENT_LLM_MODEL) { $env:AGENT_LLM_MODEL } else { 'deepseek-v4-flash' }
    $jevKey = $env:JEV_API_KEY
    $runId = Get-Date -Format 'yyyyMMddHHmmss'
    New-Item -ItemType Directory -Force -Path bin, $Out | Out-Null
    $outputPath = (Resolve-Path $Out).Path
    $agentExe = Join-Path $rootPath "bin/agent-ab-$runId.exe"
    $evalExe = Join-Path $rootPath "bin/eval-ab-$runId.exe"
    go build -o $agentExe ./cmd/agent
    if ($LASTEXITCODE -ne 0) { throw 'Agent build failed.' }
    go build -o $evalExe ./cmd/eval
    if ($LASTEXITCODE -ne 0) { throw 'Evaluator build failed.' }
    foreach ($arm in @(@{Name='baseline';Port=$BaselinePort;Jev=''}, @{Name='with-jev';Port=$ExperimentPort;Jev=$jevKey})) {
        $env:AGENT_PORT = [string]$arm.Port
        $env:JEV_API_KEY = $arm.Jev
        $processes += Start-Process -FilePath $agentExe -WorkingDirectory $rootPath -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $outputPath ($arm.Name + '.log')) -RedirectStandardError (Join-Path $outputPath ($arm.Name + '.err.log'))
        $ready = $false
        for ($i = 0; $i -lt 30; $i++) {
            try { Invoke-RestMethod -Uri "http://127.0.0.1:$($arm.Port)/healthz" -TimeoutSec 1 | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 500 }
        }
        if (!$ready) { throw "Agent $($arm.Name) did not become ready. See $outputPath." }
    }
    & $evalExe -suite intent -mode http -base "http://127.0.0.1:$BaselinePort" -key $Key -sample $Sample -workers $Workers -jev off -model $model -out "$outputPath/baseline"
    $baselineExit = $LASTEXITCODE
    & $evalExe -suite intent -mode http -base "http://127.0.0.1:$ExperimentPort" -key $Key -sample $Sample -workers $Workers -jev on -model $model -out "$outputPath/with-jev"
    $experimentExit = $LASTEXITCODE
    $b = Get-ChildItem "$outputPath/baseline/intent-*.json" | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    $e = Get-ChildItem "$outputPath/with-jev/intent-*.json" | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (!$b -or !$e) { throw 'A run did not produce a report.' }
    & $evalExe -suite compare -baseline $b.FullName -experiment $e.FullName -out $outputPath
    if ($LASTEXITCODE -ne 0 -or $baselineExit -ne 0 -or $experimentExit -ne 0) { throw 'Evaluation has failures; reports preserved for diagnosis.' }
    Write-Host "Completed: $outputPath/comparison.md"
} finally {
    foreach ($process in $processes) { if (!$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue } }
    $env:AGENT_PORT = $savedPort
    $env:JEV_API_KEY = $savedJev
    Pop-Location
}
