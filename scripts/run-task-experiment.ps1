# Default is local preparation. Only -RunEvaluation starts an agent or loads credentials.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('on','off')][string]$JevMode,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$Version,
    [Parameter(Mandatory)][string]$AgentExe,
    [Parameter(Mandatory)][string]$SourceRevision,
    [Parameter(Mandatory)][string]$Out,
    [Parameter(Mandatory)][ValidateNotNullOrEmpty()][string]$Model,
    [ValidateSet('model','jev_selective')][string]$RoutingMode,
    [ValidateRange(1,2147483647)][int]$MaxTasks = 12,
    [string]$CasesFile = 'eval/task-suites/2026-10-09-boundary-dev-v1/cases.json',
    [string]$EvaluatorExe = 'bin/task-eval-v2.exe',
    [int]$Port,
    [switch]$RunEvaluation
)
$ErrorActionPreference = 'Stop'
$rootPath = Split-Path -Parent $PSScriptRoot
$process = $null
$savedEnvironment = $null
Push-Location $rootPath
try {
    $expectedRouting = if ($JevMode -eq 'on') { 'jev_selective' } else { 'model' }
    if ($RoutingMode -and $RoutingMode -ne $expectedRouting) { throw 'RoutingMode conflicts with JevMode.' }
    $RoutingMode = $expectedRouting
    if ($RunEvaluation -and ($Port -lt 1 -or $Port -gt 65535)) { throw 'RunEvaluation requires Port between 1 and 65535.' }
    if (Test-Path -LiteralPath $Out) { throw 'Use a fresh output directory.' }
    $agentPath = (Resolve-Path -LiteralPath $AgentExe).Path
    $evaluatorPath = (Resolve-Path -LiteralPath $EvaluatorExe).Path
    $casesPath = (Resolve-Path -LiteralPath $CasesFile).Path
    $casesText = [IO.File]::ReadAllText($casesPath)
    if (!$casesText.TrimStart().StartsWith('[')) { throw 'CasesFile must contain a JSON array.' }
    $caseCount = @($casesText | ConvertFrom-Json).Count
    if ($caseCount -eq 0 -or $caseCount -gt $MaxTasks) { throw "Task count $caseCount is outside the allowed range 1..$MaxTasks." }
    $resolvedRevision = & git rev-parse --verify --end-of-options "$SourceRevision^{commit}"
    if ($LASTEXITCODE -ne 0) { throw 'SourceRevision must name an existing commit.' }
    $resolvedRevision = $resolvedRevision.Trim()
    New-Item -ItemType Directory -Path $Out | Out-Null
    $outputPath = (Resolve-Path -LiteralPath $Out).Path
    $preparePath = Join-Path $outputPath 'prepare.json'
    $commonArgs = @('-version', $Version, '-jev', $JevMode, '-routing-mode', $RoutingMode,
        '-model', $Model, '-source', $resolvedRevision, '-agent-binary', $agentPath,
        '-cases', $casesPath, '-max-tasks', [string]$MaxTasks)
    & $evaluatorPath @commonArgs -prepare-only -out $preparePath
    $prepareExit = $LASTEXITCODE
    [IO.File]::WriteAllText((Join-Path $outputPath 'prepare-exit-code.txt'), [string]$prepareExit, [Text.UTF8Encoding]::new($false))
    if ($prepareExit -ne 0) { throw "Offline preparation failed (exit $prepareExit); evidence preserved." }
    if (!(Test-Path -LiteralPath $preparePath -PathType Leaf)) { throw 'Evaluator did not create the preparation manifest.' }
    if (!$RunEvaluation) {
        Write-Host "Offline preparation complete: $preparePath"
        return
    }
    $savedEnvironment = [Environment]::GetEnvironmentVariables('Process')
    . "$PSScriptRoot/load-env.ps1"
    if (!$env:AGENT_LLM_API_KEY -or ($JevMode -eq 'on' -and !$env:JEV_API_KEY)) { throw 'Required model credentials are missing.' }
    $portProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $Port)
    $portProbe.Server.ExclusiveAddressUse = $true
    try { $portProbe.Start() } catch { throw "Port $Port cannot be bound." } finally { $portProbe.Stop() }
    $env:AGENT_PORT = [string]$Port
    $env:AGENT_LLM_MODEL = $Model
    $env:AGENT_ROUTING_MODE = $RoutingMode
    $env:JEV_FAST_PATH = '0'
    if ($JevMode -eq 'off') { $env:JEV_API_KEY = '' }
    $metadata = [ordered]@{version=$Version; jev_mode=$JevMode; routing_mode=$RoutingMode; model=$Model; source_revision=$resolvedRevision; task_count=$caseCount; max_tasks=$MaxTasks; started_at=(Get-Date).ToUniversalTime().ToString('o')}
    [IO.File]::WriteAllText((Join-Path $outputPath 'run.json'), ($metadata | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
    $process = Start-Process -FilePath $agentPath -WorkingDirectory $rootPath -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $outputPath 'agent.log') -RedirectStandardError (Join-Path $outputPath 'agent.err.log')
    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        if ($process.HasExited) { throw 'Experiment agent exited before becoming ready.' }
        try { Invoke-RestMethod -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 1 | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 500 }
    }
    if (!$ready) { throw 'Experiment agent did not become ready.' }
    & $evaluatorPath @commonArgs -base "http://127.0.0.1:$Port" -out (Join-Path $outputPath 'tasks.json') -prepared $preparePath -allow-simulated-writes
    $runExit = $LASTEXITCODE
    [IO.File]::WriteAllText((Join-Path $outputPath 'exit-code.txt'), [string]$runExit, [Text.UTF8Encoding]::new($false))
    if ($runExit -gt 1) { throw "Evaluation infrastructure failed (exit $runExit); evidence preserved." }
    Write-Host "Completed: $outputPath (evaluation exit $runExit)"
} finally {
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
    if ($null -ne $savedEnvironment) {
        foreach ($key in @([Environment]::GetEnvironmentVariables('Process').Keys)) {
            if (!$savedEnvironment.Contains($key)) { Remove-Item -LiteralPath "Env:$key" }
        }
        foreach ($key in $savedEnvironment.Keys) { [Environment]::SetEnvironmentVariable($key, $savedEnvironment[$key], 'Process') }
    }
    Pop-Location
}
