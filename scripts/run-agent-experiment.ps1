# Append-only single-version experiment. Historical reports are never selected or overwritten.
[CmdletBinding()]
param(
    [ValidateSet('on', 'off')][string]$JevMode = 'on',
    [string]$Version = 'v3',
    [string]$AgentExe = 'bin/agent.exe',
    [string]$SourceRevision = '',
    [switch]$FastPath,
    [string]$Key = 'sk-demo-8f3a2b1c9d4e5f60',
    [int]$Sample = 5,
    [int]$Workers = 4,
    [int]$Port = 19108,
    [string]$Out = ''
)
$ErrorActionPreference = 'Stop'
$rootPath = Split-Path -Parent $PSScriptRoot
$process = $null
$savedPort = $env:AGENT_PORT
$savedJev = $env:JEV_API_KEY
$savedFastPath = $env:JEV_FAST_PATH
$savedFastPathMin = $env:JEV_FAST_PATH_MIN
Push-Location $rootPath
try {
    . "$PSScriptRoot/load-env.ps1"
    $env:JEV_FAST_PATH = $(if ($FastPath) { '1' } else { '0' })
    $env:JEV_FAST_PATH_MIN = '0.90'
    if (!$env:AGENT_LLM_API_KEY -or ($JevMode -eq 'on' -and !$env:JEV_API_KEY)) { throw 'Required model credentials are missing.' }
    if ($Workers -lt 1 -or $Sample -lt 0) { throw 'Invalid sample/workers.' }
    if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) { throw "Experiment port $Port is already occupied." }
    $agentPath = (Resolve-Path -LiteralPath $AgentExe).Path
    if (!$Out) { $Out = "eval/reports/$Version-$JevMode-" + (Get-Date -Format 'yyyyMMdd-HHmmss') }
    if (Test-Path -LiteralPath $Out) { throw "Output already exists: $Out. Use a fresh directory." }
    New-Item -ItemType Directory -Path $Out | Out-Null
    $outputPath = (Resolve-Path -LiteralPath $Out).Path
    $model = $env:AGENT_LLM_MODEL
    if (!$model) { $model = 'deepseek-v4-flash' }
    $metadata = [ordered]@{
        version = $Version; jev_mode = $JevMode; model = $model; workers = $Workers; sample_per_class = $Sample
        started_at = (Get-Date).ToUniversalTime().ToString('o'); git_head = (git rev-parse HEAD)
        worktree_diff_sha256 = $null; agent_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $agentPath).Hash.ToLower()
        source_revision = $SourceRevision; source_kind = 'declared committed agent binary'
        protocol = $env:AGENT_LLM_PROTOCOL; max_tokens = $env:AGENT_LLM_MAX_TOKENS
        jev_model = $env:JEV_MODEL; fixture = 'existing development tenant; no confirmation turns or business writes'
        fast_path = [bool]$FastPath; fast_path_min = 0.90
    }
    # Preserve the exact implementation diff without exporting environment variables or credentials.
    $diffPath = Join-Path $outputPath 'implementation.patch'
    if (!$SourceRevision) { throw 'Declare the committed binary source revision with -SourceRevision.' }
    git cat-file -e "$SourceRevision^{commit}"
    if ($LASTEXITCODE -ne 0) { throw 'SourceRevision must name an existing commit.' }
    $diff = @("Agent binary source revision declared by caller: $SourceRevision. Worktree changes do not describe this binary.")
    [IO.File]::WriteAllText($diffPath, ($diff -join "`n"), [Text.UTF8Encoding]::new($false))
    $metadata.worktree_diff_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $diffPath).Hash.ToLower()
    [IO.File]::WriteAllText((Join-Path $outputPath 'run.json'), ($metadata | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
    $env:AGENT_PORT = [string]$Port
    if ($JevMode -eq 'off') { $env:JEV_API_KEY = '' }
    $process = Start-Process -FilePath $agentPath -WorkingDirectory $rootPath -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $outputPath 'agent.log') -RedirectStandardError (Join-Path $outputPath 'agent.err.log')
    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        try { Invoke-RestMethod -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 1 | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 500 }
    }
    if (!$ready) { throw 'Experiment agent did not become ready.' }
    go run ./cmd/eval -suite intent -mode http -base "http://127.0.0.1:$Port" -key $Key -sample $Sample -workers $Workers -jev $JevMode -model $model -out $outputPath
    $runExit = $LASTEXITCODE
    [IO.File]::WriteAllText((Join-Path $outputPath 'exit-code.txt'), [string]$runExit, [Text.UTF8Encoding]::new($false))
    if ($runExit -ne 0) { throw "Experiment contains failures (exit $runExit); all reports preserved at $outputPath." }
    Write-Host "Experiment completed: $outputPath"
} finally {
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
    $env:AGENT_PORT = $savedPort
    $env:JEV_API_KEY = $savedJev
    $env:JEV_FAST_PATH = $savedFastPath
    $env:JEV_FAST_PATH_MIN = $savedFastPathMin
    Pop-Location
}
