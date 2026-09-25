# 对比两次意图评测报告（JEV 前 vs 后）。
#
#   .\scripts\compare-intent.ps1 -Baseline eval\reports\baseline.md -Experiment eval\reports\with-jev.md
#
# 从两份 Markdown 报告里抽出核心指标，输出对照表与差值。
# 只解析「核心指标」章节 —— 报告格式由 cmd/eval 生成，两边一致。

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Baseline,
    [Parameter(Mandatory = $true)][string]$Experiment
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

function Get-Metrics {
    param([string]$Path)
    if (-not (Test-Path $Path)) { throw "报告不存在: $Path" }
    $text = [System.IO.File]::ReadAllText((Resolve-Path $Path), [System.Text.Encoding]::UTF8)
    $m = @{}
    # 形如：| Top-1 准确率 | **75.5%** |  或  | Macro-F1 | 0.605 |
    foreach ($line in ($text -split "`r?`n")) {
        if ($line -notmatch '^\|(.+)\|(.+)\|$') { continue }
        $k = $Matches[1].Trim()
        $v = $Matches[2].Trim() -replace '\*\*', ''
        if ($k -in @('指标', '---')) { continue }
        # 提取数值
        if ($v -match '^([\d.]+)%$') { $m[$k] = [double]$Matches[1] }
        elseif ($v -match '^([\d.]+)$') { $m[$k] = [double]$Matches[1] }
        elseif ($v -match '^(\d+) 条$') { $m[$k] = [double]$Matches[1] }
    }
    return $m
}

$b = Get-Metrics $Baseline
$e = Get-Metrics $Experiment

# 指标名 → 方向（+1 越大越好，-1 越小越好，0 仅观察）
$dir = [ordered]@{
    'Top-1 准确率'          = 1
    'Macro-F1'              = 1
    'Macro-Precision'       = 1
    'Macro-Recall'          = 1
    'OOS 准确率（闲聊零工具调用）' = 1
    'OOS 误调用率'          = -1
    '对抗拦截率'            = 1
    '正常请求误拦率'        = -1
    '写操作确认覆盖率'      = 1
    '平均延迟'              = 0
    '平均工具调用次数'      = 0
    'JEV 短路（未调 LLM）'  = 0
}

Write-Host ""
Write-Host "=== JEV 前后对比 ===" -ForegroundColor Cyan
Write-Host ("基线: {0}" -f (Split-Path $Baseline -Leaf))
Write-Host ("实验: {0}" -f (Split-Path $Experiment -Leaf))
Write-Host ""
Write-Host ("{0,-30} {1,>12} {2,>12} {3,>12}  {4}" -f '指标', '关闭 JEV', '开启 JEV', '差值', '判定')
Write-Host ("-" * 84)

$better = 0; $worse = 0
foreach ($k in $dir.Keys) {
    if (-not $b.ContainsKey($k) -or -not $e.ContainsKey($k)) { continue }
    $bv = $b[$k]; $ev = $e[$k]
    $diff = $ev - $bv
    $sign = if ($diff -gt 0) { '+' } else { '' }

    $verdict = '—'
    if ($dir[$k] -ne 0 -and [Math]::Abs($diff) -gt 0.0001) {
        $isBetter = ($dir[$k] -gt 0 -and $diff -gt 0) -or ($dir[$k] -lt 0 -and $diff -lt 0)
        if ($isBetter) { $verdict = '改善'; $better++ } else { $verdict = '变差'; $worse++ }
    }

    $color = 'Gray'
    if ($verdict -eq '改善') { $color = 'Green' }
    elseif ($verdict -eq '变差') { $color = 'Red' }

    Write-Host ("{0,-30} {1,12} {2,12} {3,12}  {4}" -f $k, $bv, $ev, "$sign$([Math]::Round($diff,3))", $verdict) -ForegroundColor $color
}

Write-Host ""
Write-Host ("改善 {0} 项，变差 {1} 项" -f $better, $worse) -ForegroundColor $(if ($worse -eq 0) { 'Green' } else { 'Yellow' })
Write-Host ""
Write-Host "提示：'正常请求误拦率' 变差要优先排查 —— 安全策略伤到正常业务比漏放更严重。" -ForegroundColor DarkGray
