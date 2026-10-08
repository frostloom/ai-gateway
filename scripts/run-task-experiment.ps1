# State-verified live tasks; all writes target newly created simulation tenants.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('on','off')][string]$JevMode,
    [Parameter(Mandatory)][string]$Version,
    [Parameter(Mandatory)][string]$AgentExe,
    [Parameter(Mandatory)][string]$SourceRevision,
    [Parameter(Mandatory)][int]$Port,
    [Parameter(Mandatory)][string]$Out,
    [string]$EvaluatorExe = 'bin/task-eval-v2.exe'
)
$ErrorActionPreference = 'Stop'
$rootPath = Split-Path -Parent $PSScriptRoot
$process = $null
$savedPort = $env:AGENT_PORT
$savedJev = $env:JEV_API_KEY
Push-Location $rootPath
try {
    . "$PSScriptRoot/load-env.ps1"
    if (!$env:AGENT_LLM_API_KEY -or ($JevMode -eq 'on' -and !$env:JEV_API_KEY)) { throw 'Required model credentials are missing.' }
    if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) { throw "Port $Port is occupied." }
    if (Test-Path -LiteralPath $Out) { throw 'Use a fresh output directory.' }
    $agentPath = (Resolve-Path -LiteralPath $AgentExe).Path
    $evaluatorPath = (Resolve-Path -LiteralPath $EvaluatorExe).Path
    New-Item -ItemType Directory -Path $Out | Out-Null
    $outputPath = (Resolve-Path -LiteralPath $Out).Path
    $env:AGENT_PORT = [string]$Port
    if ($JevMode -eq 'off') { $env:JEV_API_KEY = '' }
    $process = Start-Process -FilePath $agentPath -WorkingDirectory $rootPath -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $outputPath 'agent.log') -RedirectStandardError (Join-Path $outputPath 'agent.err.log')
    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        try { Invoke-RestMethod -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 1 | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 500 }
    }
    if (!$ready) { throw 'Experiment agent did not become ready.' }
    & $evaluatorPath -base "http://127.0.0.1:$Port" -out (Join-Path $outputPath 'tasks.json') -version $Version -jev $JevMode -model $env:AGENT_LLM_MODEL -source $SourceRevision -agent-binary $agentPath -allow-simulated-writes
    $runExit = $LASTEXITCODE
    [IO.File]::WriteAllText((Join-Path $outputPath 'exit-code.txt'), [string]$runExit, [Text.UTF8Encoding]::new($false))
    if ($runExit -gt 1) { throw "Evaluation infrastructure failed (exit $runExit); evidence preserved." }
    Write-Host "Completed: $outputPath (evaluation exit $runExit)"
} finally {
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
    $env:AGENT_PORT = $savedPort
    $env:JEV_API_KEY = $savedJev
    Pop-Location
}
