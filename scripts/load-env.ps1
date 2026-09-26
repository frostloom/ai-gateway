# 载入 .env 到当前进程环境变量。
#
#   . .\scripts\load-env.ps1          # dot-source（在当前作用域生效）
#
# 为什么单独成文件：
#   1) 这段逻辑以前在多处复制粘贴，改一处漏一处；
#   2) .env 里有的值带引号（如 MYSQL_DSN="root:...&loc=Local"），shell 的 source
#      会剥掉引号，PowerShell 不会 —— 不处理就会出现字面量 \" 导致连不上库；
#   3) 需要跳过注释行，但**行尾的 # 注释不能当注释**（值里可能含 #）。
#
# 注意：必须 dot-source（`. path`）才会影响调用方作用域；
# 直接 `& path` 在子作用域里，环境变量不会留给你。

$envPath = Join-Path (Split-Path -Parent $PSScriptRoot) '.env'
if (-not (Test-Path $envPath)) {
    Write-Warning "未找到 $envPath"
    return
}

$count = 0
foreach ($line in [System.IO.File]::ReadAllLines($envPath, [System.Text.Encoding]::UTF8)) {
    $t = $line.Trim()
    if ($t -eq '' -or $t.StartsWith('#')) { continue }
    $i = $t.IndexOf('=')
    if ($i -le 0) { continue }

    $k = $t.Substring(0, $i).Trim()
    $v = $t.Substring($i + 1).Trim()

    # 只去成对引号，不去行尾注释（值里可能有 #，如密码）
    if ($v.Length -ge 2) {
        if (($v[0] -eq '"' -and $v[-1] -eq '"') -or ($v[0] -eq "'" -and $v[-1] -eq "'")) {
            $v = $v.Substring(1, $v.Length - 2)
        }
    }
    if ($k -match '^[A-Za-z_][A-Za-z0-9_]*$') {
        Set-Item -Path "env:$k" -Value $v
        $count++
    }
}
Write-Host "已载入 $count 个环境变量（来自 .env）"
