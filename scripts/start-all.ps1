# ai-gateway 一键启动（Windows PowerShell）。
#
#   .\scripts\start-all.ps1              # 启动（幂等：先杀掉在跑的再拉起）
#   .\scripts\start-all.ps1 -Rebuild     # 先 go build 再启动
#   .\scripts\start-all.ps1 -Only agent  # 只重启指定服务（逗号分隔）
#
# 与 scripts/start-all.sh 等价。存在的理由：这台机器上 bash 走 WSL relay 不可用，
# 而用 PowerShell 逐个 export .env 有坑 —— .env 里有的值带双引号
# （如 MYSQL_DSN="root:...&loc=Local"），shell 的 source 会剥掉引号，
# PowerShell 不会，导致 DSN 里出现字面量 \" 而连不上库。
# 本脚本的 Import-DotEnv 负责正确去引号。
#
# 注意：不要用 PowerShell 的 Get-Content/Set-Content 去改写源码文件 —— 本机
# PowerShell 5.1 默认 GBK，会把无 BOM 的 UTF-8 源码写坏（见 web/scripts/repair-encoding.ps1）。
# 本脚本只读 .env、只写 logs/，不碰源码。

[CmdletBinding()]
param(
    [switch]$Rebuild,
    [string]$Only = "",
    [switch]$SkipInfra
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
New-Item -ItemType Directory -Force -Path logs, bin | Out-Null

# ---------- .env 载入（正确处理引号） ----------
function Import-DotEnv {
    param([string]$Path)
    if (-not (Test-Path $Path)) { Write-Host "==> 未找到 .env，跳过" -ForegroundColor Yellow; return }
    Write-Host "==> 载入 .env"
    foreach ($line in [System.IO.File]::ReadAllLines($Path, [System.Text.Encoding]::UTF8)) {
        $t = $line.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        $i = $t.IndexOf('=')
        if ($i -le 0) { continue }
        $k = $t.Substring(0, $i).Trim()
        $v = $t.Substring($i + 1).Trim()
        # 去成对引号（shell source 也这么做）
        if ($v.Length -ge 2) {
            if (($v[0] -eq '"' -and $v[-1] -eq '"') -or ($v[0] -eq "'" -and $v[-1] -eq "'")) {
                $v = $v.Substring(1, $v.Length - 2)
            }
        }
        if ($k) { Set-Item -Path "env:$k" -Value $v }
    }
}
Import-DotEnv (Join-Path $Root '.env')

# ---------- 工具函数 ----------
function Stop-Port {
    param([int]$Port)
    $conns = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    foreach ($c in $conns) {
        try { Stop-Process -Id $c.OwningProcess -Force -ErrorAction Stop } catch {}
    }
}

function Start-Svc {
    param([string]$Name, [string]$Exe, [string[]]$Args = @(), [hashtable]$Env = @{})
    $path = Join-Path $Root "bin\$Exe"
    if (-not (Test-Path $path)) { Write-Host "  ✗ $Name 缺少 $Exe（先 -Rebuild）" -ForegroundColor Red; return }
    # 临时注入该服务专属环境变量
    $saved = @{}
    foreach ($k in $Env.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k); Set-Item -Path "env:$k" -Value $Env[$k] }
    $sp = @{
        FilePath               = $path
        WorkingDirectory       = $Root
        WindowStyle            = 'Hidden'
        RedirectStandardOutput = Join-Path $Root "logs\$Name.log"
        RedirectStandardError  = Join-Path $Root "logs\$Name.err.log"
    }
    if ($Args.Count -gt 0) { $sp.ArgumentList = $Args }
    Start-Process @sp
    foreach ($k in $saved.Keys) {
        if ($null -eq $saved[$k]) { Remove-Item -Path "env:$k" -ErrorAction SilentlyContinue }
        else { Set-Item -Path "env:$k" -Value $saved[$k] }
    }
}

function Wait-Port {
    param([int]$Port, [string]$Name)
    for ($i = 0; $i -lt 20; $i++) {
        if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
            Write-Host ("  OK   {0,-18} :{1}" -f $Name, $Port) -ForegroundColor Green
            return $true
        }
        Start-Sleep -Seconds 1
    }
    Write-Host ("  FAIL {0,-18} :{1}  (看 logs\{0}.log)" -f $Name, $Port) -ForegroundColor Red
    return $false
}

# ---------- 基础设施 ----------
if (-not $SkipInfra) {
    # docker compose 会把「Container xxx Running」这类正常进度写到 stderr，
    # 在 $ErrorActionPreference='Stop' 下会被当成错误而中断。这里局部放开。
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $running = docker ps --format '{{.Names}}' 2>$null
    if ($running -notcontains 'ai-gateway-mysql') {
        Write-Host "==> docker compose up -d mysql redis kafka kafka-init"
        docker compose up -d mysql redis kafka kafka-init | Out-Null
    } else {
        Write-Host "==> MySQL/Redis 已在跑（补起 kafka）"
        docker compose up -d kafka kafka-init | Out-Null
    }
    Write-Host -NoNewline "==> 等 MySQL 就绪"
    $ok = $false
    for ($i = 0; $i -lt 30; $i++) {
        docker exec ai-gateway-mysql mysqladmin ping -uroot -proot --silent *> $null
        if ($LASTEXITCODE -eq 0) { $ok = $true; break }
        Start-Sleep -Seconds 2
    }
    $ErrorActionPreference = $prevEAP
    if (-not $ok) { Write-Host " 超时" -ForegroundColor Red; exit 1 }
    Write-Host " ok"
}

# ---------- 构建 ----------
if ($Rebuild) {
    Write-Host "==> go build -o bin/ ./cmd/..."
    if (-not $env:GOCACHE) { $env:GOCACHE = Join-Path $Root '.gocache' }
    & go build -o bin/ ./cmd/...
    if ($LASTEXITCODE -ne 0) { Write-Host "构建失败" -ForegroundColor Red; exit 1 }
}

# ---------- 服务表 ----------
$gatewayPort = if ($env:GATEWAY_PORT) { [int]$env:GATEWAY_PORT } else { 18080 }
$agentPort   = if ($env:AGENT_PORT)   { [int]$env:AGENT_PORT }   else { 9105 }

$services = @(
    @{ Name='billing';        Exe='billing.exe';        Ports=@(9101,9103); Args=@() }
    @{ Name='router';         Exe='router.exe';         Ports=@(9102);      Args=@() }
    @{ Name='mock-a';         Exe='mock-provider.exe';  Ports=@(9201);      Args=@(); Env=@{MOCK_PROVIDER_PORT='9201';MOCK_PROVIDER_NAME='mock-a'} }
    @{ Name='mock-b';         Exe='mock-provider.exe';  Ports=@(9202);      Args=@(); Env=@{MOCK_PROVIDER_PORT='9202';MOCK_PROVIDER_NAME='mock-b'} }
    @{ Name='mock-c';         Exe='mock-provider.exe';  Ports=@(9203);      Args=@(); Env=@{MOCK_PROVIDER_PORT='9203';MOCK_PROVIDER_NAME='mock-c'} }
    @{ Name='gateway';        Exe='gateway.exe';        Ports=@($gatewayPort); Args=@() }
    @{ Name='reconciler';     Exe='reconciler.exe';     Ports=@(9106);      Args=@('-interval','30','-grace','600') }
    @{ Name='event-consumer'; Exe='event-consumer.exe'; Ports=@(9107);      Args=@() }
    @{ Name='agent';          Exe='agent.exe';          Ports=@($agentPort); Args=@() }
)

$targets = $services
if ($Only) {
    $want = $Only.Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ }
    $targets = $services | Where-Object { $want -contains $_.Name }
    if (-not $targets) { Write-Host "未匹配到服务：$Only" -ForegroundColor Red; exit 1 }
}

# ---------- 清理旧进程 ----------
Write-Host "==> 清理已在跑的旧服务"
foreach ($s in $targets) { foreach ($p in $s.Ports) { Stop-Port -Port $p } }
Start-Sleep -Milliseconds 800

# ---------- 启动 ----------
Write-Host "==> 启动服务（日志 logs/*.log）"
foreach ($s in $targets) {
    $envArgs = if ($s.ContainsKey('Env')) { $s.Env } else { @{} }
    Start-Svc -Name $s.Name -Exe $s.Exe -Args $s.Args -Env $envArgs
}

# ---------- 就绪检查 ----------
Write-Host "==> 服务状态"
$failed = 0
foreach ($s in $targets) {
    foreach ($p in $s.Ports) {
        if (-not (Wait-Port -Port $p -Name $s.Name)) { $failed++ }
    }
}

Write-Host ""
if ($failed -eq 0) {
    Write-Host "完成。打开：" -ForegroundColor Green
    Write-Host "  管理面板    http://localhost:$gatewayPort/         (账号 zlx)"
    Write-Host "  用户自助页  http://localhost:$gatewayPort/portal   (key sk-demo-8f3a2b1c9d4e5f60)"
} else {
    Write-Host "$failed 个端口未就绪，检查 logs/ 下对应 .log / .err.log" -ForegroundColor Yellow
}
$agentLog = Join-Path $Root 'logs\agent.log'
if (Test-Path $agentLog) {
    $line = Select-String -Path $agentLog -Pattern 'agent service listening' | Select-Object -Last 1
    if ($line) { Write-Host "  agent: $($line.Line.Trim())" }
}
