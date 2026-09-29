# Compare matching schema-v2 JSON reports; validates pairing and server telemetry.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Baseline,
    [Parameter(Mandatory = $true)][string]$Experiment,
    [string]$Out = 'eval/reports/comparison'
)
$ErrorActionPreference = 'Stop'
go run ./cmd/eval -suite compare -baseline $Baseline -experiment $Experiment -out $Out
if ($LASTEXITCODE -ne 0) { throw 'Intent comparison failed. Use matching schema-v2 JSON reports.' }
