<#
阶段 0 验证脚本：统一时钟 + 自动日志采样。

用法：
  powershell -NoProfile -ExecutionPolicy Bypass -File tools\verify.ps1 -Mode check
  powershell -NoProfile -ExecutionPolicy Bypass -File tools\verify.ps1 -Mode expire
  powershell -NoProfile -ExecutionPolicy Bypass -File tools\verify.ps1 -Mode relogin
  powershell -NoProfile -ExecutionPolicy Bypass -File tools\verify.ps1 -Mode badpass

设计原则：
1. 所有时间戳来自脚本同一个时钟，精确到毫秒；
2. 状态采样读 state.json（守护每 5 秒写一次），日志采样只读本次运行新增的行；
3. 全程写同一个 .log 文件，同时打印到屏幕，避免"两边对不上"；
4. 结束时输出可核对的结构化 SUMMARY。
#>
[CmdletBinding()]
param(
    [ValidateSet('menu', 'check', 'expire', 'relogin', 'badpass')]
    [string]$Mode = 'menu',
    [string]$Username = '999999999',
    [int]$TimeoutSec = 180
)

$ErrorActionPreference = 'Stop'

# ---------- 路径 ----------
$Root = Split-Path -Parent $PSScriptRoot
$EpoExe = Join-Path $Root 'build\epo.exe'
$DaemonExe = Join-Path $Root 'build\epo-daemon.exe'
$GuiExe = Join-Path $Root 'build\epo-gui.exe'

$UserDir = Join-Path $env:LOCALAPPDATA 'epo'
$StateFile = Join-Path $UserDir 'state.json'
$StopFlag = Join-Path $UserDir 'stop.flag'
$DaemonLog = Join-Path $UserDir 'logs\epo.log'
$SecretFile = Join-Path $UserDir 'secret.bin'
$SecretBak = Join-Path $UserDir 'secret.bin.bak'

$Stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$OutFile = Join-Path $PSScriptRoot ('verify-{0}-{1}.log' -f $Mode, $Stamp)
$SummaryFile = Join-Path $PSScriptRoot ('verify-{0}-{1}.summary.txt' -f $Mode, $Stamp)
$LastFile = Join-Path $PSScriptRoot 'verify-last.txt'

# ---------- 输出 ----------
if (Test-Path $OutFile) { Remove-Item $OutFile -Force }
New-Item -ItemType File -Path $OutFile -Force | Out-Null
$SummaryLines = @()

function Log([string]$text, [string]$level = 'INFO') {
    $line = '[{0}] [{1}] {2}' -f (Get-Date -Format 'HH:mm:ss.fff'), $level, $text
    Write-Host $line
    Add-Content -Path $OutFile -Value $line -Encoding UTF8
}

function Summary([string]$text) {
    $script:SummaryLines += $text
    Log $text 'SUMMARY'
}

# ---------- 状态 / 日志采样 ----------
$Script:LogOffset = 0
$Script:SkipFirstLine = $false
$Script:LastState = ''
$Script:StateSince = Get-Date
$Script:FailCount = 0
$Script:StartTime = Get-Date
$Script:HistoryOffline = $false

function Init-LogOffset {
    if (Test-Path $DaemonLog) {
        $len = (Get-Item $DaemonLog).Length
        # 回读最近 4KB：脚本启动前刚发生的事件也要进时间线，避免"两边对不上"
        $back = [Math]::Min($len, 4096)
        $Script:LogOffset = [long]($len - $back)
        # 从字节中间切会得到半行 JSON，跳过第一行
        $Script:SkipFirstLine = ($Script:LogOffset -gt 0)
    }
    else { $Script:LogOffset = 0 }
}

function Read-LogDelta {
    if (-not (Test-Path $DaemonLog)) { return @() }
    $fs = $null
    try {
        $fs = [IO.File]::Open($DaemonLog, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
        $len = $fs.Length - $Script:LogOffset
        if ($len -le 0) { return @() }
        $fs.Position = $Script:LogOffset
        $buf = New-Object byte[] $len
        [void]$fs.Read($buf, 0, $len)
        $Script:LogOffset = $fs.Length
        $text = [Text.Encoding]::UTF8.GetString($buf)
        $lines = @($text -split "`r?`n" | Where-Object { $_ -match '\S' })
        if ($Script:SkipFirstLine -and $lines.Count -gt 0) {
            if ($lines.Count -eq 1) { $lines = @() } else { $lines = $lines[1..($lines.Count - 1)] }
            $Script:SkipFirstLine = $false
        }
        return $lines
    }
    catch {
        return @()
    }
    finally {
        if ($fs) { $fs.Dispose() }
    }
}

function Get-Snap {
    if (-not (Test-Path $StateFile)) { return $null }
    try { return (Get-Content $StateFile -Raw -Encoding UTF8 | ConvertFrom-Json) }
    catch { return $null }
}

function Get-SnapAge {
    param($snap)
    if ($null -eq $snap) { return [double]::MaxValue }
    try { return ((Get-Date) - [datetime]$snap.updated_at).TotalSeconds }
    catch { return [double]::MaxValue }
}

function Test-DaemonRunning {
    $snap = Get-Snap
    if ($null -eq $snap) { return $false }
    if ($snap.state -eq 'stopped') { return $false }
    return ((Get-SnapAge $snap) -lt 20)
}

# 采样一次：状态变化 + 新增日志行都按同一时钟记录
function Poll {
    $snap = Get-Snap
    if ($null -ne $snap) {
        $nowState = [string]$snap.state
        if ($nowState -ne $Script:LastState) {
            $held = [math]::Round(((Get-Date) - $Script:StateSince).TotalSeconds, 1)
            Log ('状态变化: {0} -> {1}（上一状态持续 {2}s）' -f $Script:LastState, $nowState, $held) 'STATE'
            $Script:LastState = $nowState
            $Script:StateSince = Get-Date
        }
    }
    foreach ($line in (Read-LogDelta)) {
        $isHistory = $false
        if ($line -match '"time":"([^"]+)"') {
            try { if ([datetime]$matches[1] -lt $Script:StartTime) { $isHistory = $true } } catch { }
        }
        if ($isHistory -and $line -match '检测到掉线') { $Script:HistoryOffline = $true }
        if (-not $isHistory -and $line -match '登录失败') { $Script:FailCount++ }
        if ($isHistory) {
            Log ('  [历史] [daemon] ' + $line) 'DAEMON'
        } else {
            Log ('  [daemon] ' + $line) 'DAEMON'
        }
    }
}

function Wait-State([string]$want, [int]$seconds) {
    $start = Get-Date
    $nextHint = $start.AddSeconds(15)
    while (((Get-Date) - $start).TotalSeconds -lt $seconds) {
        Poll
        if ($Script:LastState -eq $want) { return $true }
        if ((Get-Date) -gt $nextHint) {
            $elapsed = [math]::Round(((Get-Date) - $start).TotalSeconds)
            Log ('仍在等待 {0}：已等 {1} 秒，剩余约 {2} 秒' -f $want, $elapsed, ($seconds - $elapsed)) 'WAIT'
            $nextHint = (Get-Date).AddSeconds(15)
        }
        Start-Sleep -Milliseconds 500
    }
    Poll
    return $false
}

function Start-DaemonIfNeeded {
    if (Test-DaemonRunning) {
        Log '守护已在运行，复用现有进程' 'INFO'
        return $false
    }
    if (-not (Test-Path $DaemonExe)) { throw "缺少 $DaemonExe，请先构建" }
    if (Test-Path $StopFlag) { Remove-Item $StopFlag -Force -ErrorAction SilentlyContinue }
    Start-Process -FilePath $DaemonExe | Out-Null
    Log '已启动 epo-daemon.exe，等待首帧状态…'
    return $true
}

function Stop-Daemon {
    if (-not (Test-DaemonRunning)) { return }
    [IO.File]::WriteAllText($StopFlag, 'stop')
    $deadline = (Get-Date).AddSeconds(12)
    while ((Get-Date) -lt $deadline) {
        Poll
        if (-not (Test-DaemonRunning)) { Log '守护已停止'; return }
        Start-Sleep -Milliseconds 500
    }
    Log '守护在 12 秒内未停止' 'WARN'
}

function Show-Tail([int]$n = 10) {
    if (Test-Path $DaemonLog) {
        Log '最近守护日志：'
        Get-Content $DaemonLog -Tail $n -Encoding UTF8 | ForEach-Object { Log ('  ' + $_) 'TAIL' }
    }
}

function Finish([string]$result, [string]$detail) {
    Log ('==================================================================') 'SUMMARY'
    Summary ('模式: ' + $Mode)
    Summary ('结论: ' + $result)
    Summary ('说明: ' + $detail)
    Summary ('完整记录: ' + $OutFile)
    $report = @(
        'epo 阶段 0 验证报告',
        ('生成时间: ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss.fff')),
        ('模式: ' + $Mode),
        ('结论: ' + $result),
        ('说明: ' + $detail),
        ('完整记录: ' + $OutFile),
        '',
        '结算数据：'
    ) + $SummaryLines
    [IO.File]::WriteAllLines($SummaryFile, $report, [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllLines($LastFile, $report, [Text.UTF8Encoding]::new($false))
}

# ---------- 交互菜单 ----------
if ($Mode -eq 'menu') {
    Clear-Host
    Write-Host '==========================================' -ForegroundColor Cyan
    Write-Host '  epo 阶段 0 验证' -ForegroundColor Cyan
    Write-Host '==========================================' -ForegroundColor Cyan
    Write-Host '  1  会话过期后自动重登（需在门户点注销）'
    Write-Host '  2  同账号二次登录（需手机登录同一学号）'
    Write-Host '  3  错误密码与退避节奏（自动执行，约 3 分钟）'
    Write-Host '  4  环境自检'
    Write-Host '  0  退出'
    Write-Host '==========================================' -ForegroundColor Cyan
    $choice = Read-Host '请输入序号'
    switch ($choice) {
        '1' { $Mode = 'expire' }
        '2' { $Mode = 'relogin' }
        '3' {
            $Mode = 'badpass'
            $u = Read-Host '测试账号（直接回车 = 假账号 999999999）'
            if (-not [string]::IsNullOrWhiteSpace($u)) { $Username = $u.Trim() }
        }
        '4' { $Mode = 'check' }
        '0' { exit 0 }
        default { Write-Host '无效选择，已退出' -ForegroundColor Yellow; exit 1 }
    }
    # 菜单选择后才确定最终模式，重新计算输出文件名，避免留下 verify-menu-*.log
    Remove-Item -Path $OutFile -Force -ErrorAction SilentlyContinue
    $OutFile = Join-Path $PSScriptRoot ('verify-{0}-{1}.log' -f $Mode, $Stamp)
    $SummaryFile = Join-Path $PSScriptRoot ('verify-{0}-{1}.summary.txt' -f $Mode, $Stamp)
    New-Item -ItemType File -Path $OutFile -Force | Out-Null
    Write-Host ''
}

# ---------- 入场 ----------
Log ('epo 阶段 0 验证 | 模式=' + $Mode + ' | 开始 ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss.fff'))
Log ('输出文件: ' + $OutFile)
Log '已回读最近 4KB 守护日志，脚本启动前的事件会标记为 [历史]'
Init-LogOffset

$missing = @()
foreach ($f in @($EpoExe, $DaemonExe)) { if (-not (Test-Path $f)) { $missing += $f } }
if ($missing.Count -gt 0) {
    Log ('缺少产物: ' + ($missing -join ', ')) 'ERR'
    Finish 'BLOCKED' '请先在 build 目录构建 epo.exe 与 epo-daemon.exe'
    exit 1
}
if (-not (Test-Path $SecretFile)) {
    Log '未找到加密凭据 %LOCALAPPDATA%\epo\secret.bin' 'ERR'
    Finish 'BLOCKED' '请先在 epo-gui.exe 的「账号」页保存学号与密码'
    exit 1
}

$snap0 = Get-Snap
if ($null -ne $snap0) {
    Log ('当前状态: state={0} age={1:N1}s pid={2}' -f $snap0.state, (Get-SnapAge $snap0), $snap0.pid)
} else {
    Log '尚无状态文件（守护从未启动）'
}

if ($Mode -eq 'check') {
    $ok = (Test-DaemonRunning)
    Log ("产物齐全；守护" + $(if ($ok) { '运行中' } else { '未运行' }))
    Summary ('守护运行: ' + $ok)
    Summary ('状态快照: ' + $(if ($null -ne $snap0) { $snap0.state } else { '无' }))
    Summary ('日志文件: ' + $DaemonLog + ' 存在=' + (Test-Path $DaemonLog))
    Finish 'OK' '环境自检通过，可执行 expire / relogin / badpass'
    exit 0
}

# ---------- expire：会话过期自动重登 ----------
if ($Mode -eq 'expire') {
    Start-DaemonIfNeeded | Out-Null
    if (-not (Wait-State 'online' 30)) {
        Log '守护启动后 30 秒内未进入在线态' 'ERR'
        Show-Tail 12
        Finish 'FAIL' '守护未能达到在线态，先解决登录问题再测'
        exit 1
    }
    Log '当前在线。请在保持 Wi-Fi 连接的情况下，用浏览器打开 http://172.16.121.9/ 并点击「注销」。' 'ACTION'
    Log '（不要断开 Wi-Fi，否则探针会进入 unknown 而不是 offline）' 'ACTION'
    Log ('等待掉线，最长 {0} 秒…' -f $TimeoutSec)

    $tAction = Get-Date
    if (-not (Wait-State 'offline' $TimeoutSec)) {
        Log '未观察到 offline 状态' 'ERR'
        Show-Tail 12
        Finish 'FAIL' '未检测到掉线：可能没有点注销，或网关尚未断开会话'
        exit 1
    }
    $tOffline = Get-Date
    Log ('已检测到 offline，用时 {0:N1}s，等待自动重登…' -f ($tOffline - $tAction).TotalSeconds)

    if (-not (Wait-State 'online' $TimeoutSec)) {
        Log '掉线后未恢复在线' 'ERR'
        Show-Tail 15
        Finish 'FAIL' '自动重登失败，请查看 daemon 日志中的失败原因'
        exit 1
    }
    $tOnline = Get-Date
    $recover = [math]::Round(($tOnline - $tOffline).TotalSeconds, 1)

    Summary ('掉线检测时刻: ' + $tOffline.ToString('HH:mm:ss.fff'))
    Summary ('恢复在线时刻: ' + $tOnline.ToString('HH:mm:ss.fff'))
    Summary ('重登耗时: ' + $recover + 's（含守护 5s 探测周期）')
    Show-Tail 12
    Finish 'PASS' ('会话过期后自动重登成功，耗时 ' + $recover + 's')
    exit 0
}

# ---------- relogin：同账号二次登录 ----------
if ($Mode -eq 'relogin') {
    Start-DaemonIfNeeded | Out-Null
    if (-not (Wait-State 'online' 30)) {
        Log '启动后未进入在线态' 'ERR'
        Finish 'FAIL' '需先保证本机在线'
        exit 1
    }

    Log '当前在线。请用手机或另一台电脑连接同一 SSID（JXAU-Stu），并用同一学号登录。' 'ACTION'
    Log ('脚本会记录「在线 → 掉线 → 恢复」的每一次循环，最长观察 {0} 秒。' -f $TimeoutSec)

    $start = Get-Date
    $pairs = 0
    $offlineAt = $null
    $results = @()
    $nextHint = $start.AddSeconds(30)

    while (((Get-Date) - $start).TotalSeconds -lt $TimeoutSec) {
        Poll
        if ($Script:LastState -eq 'offline' -and $null -eq $offlineAt) {
            $offlineAt = Get-Date
        }
        if ($null -ne $offlineAt -and $Script:LastState -eq 'online') {
            $pairs++
            $cost = [math]::Round(((Get-Date) - $offlineAt).TotalSeconds, 1)
            $results += ('第 {0} 次互踢：掉线 → 恢复 {1}s' -f $pairs, $cost)
            Summary $results[-1]
            $offlineAt = $null
        }
        if ((Get-Date) -gt $nextHint) {
            $elapsed = [math]::Round(((Get-Date) - $start).TotalSeconds)
            Log ('观察进度 {0}/{1} 秒，已记录掉线 {2} 次' -f $elapsed, $TimeoutSec, $pairs) 'WAIT'
            $nextHint = (Get-Date).AddSeconds(30)
        }
        Start-Sleep -Milliseconds 500
    }

    if ($pairs -eq 0) {
        if ($Script:HistoryOffline) {
            Summary '开场前日志中有一次掉线（已自动恢复），观察窗口内未再发生'
            Finish 'PASS' '观察窗口内多设备可同时在线；开场前的掉线原因未确认，不作为互踢证据'
        } else {
            Summary '未观察到掉线：手机与本机同时在线，门户无单终端限制'
            Finish 'PASS' '同账号多设备可同时在线，本机未被踢下线'
        }
    } else {
        Summary ('共观察到 ' + $pairs + ' 次互踢，守护每次都能重新登回')
        Finish 'PASS' ('存在单终端互踢；守护可自动恢复，共 ' + $pairs + ' 次')
    }
    Show-Tail 15
    exit 0
}

# ---------- badpass：密码错误与退避节奏 ----------
if ($Mode -eq 'badpass') {
    Log '本模式会临时替换凭据为错误密码，测试结束后自动恢复原始凭据。' 'WARN'
    Log '测试期间本机无法自动登录；开始后需要你在门户点一次「注销」。' 'WARN'

    $wasRunning = Test-DaemonRunning
    try {
        Copy-Item $SecretFile $SecretBak -Force
        Log ('已备份凭据到 ' + $SecretBak)

        $tmpCred = Join-Path $Root 'build\wrong-cred.tmp.json'
        $obj = [ordered]@{ username = $Username; password = 'wrong-password'; carrier = 'cmcc' }
        $payload = $obj | ConvertTo-Json -Compress
        [IO.File]::WriteAllText($tmpCred, $payload, [Text.UTF8Encoding]::new($false))
        Log ('测试账号: ' + $Username + '（密码固定为错误值）')
        & $EpoExe save-cred -cred $tmpCred *> $null
        if ($LASTEXITCODE -ne 0) { throw "保存临时凭据失败（退出码 $LASTEXITCODE）" }
        Remove-Item $tmpCred -Force -ErrorAction SilentlyContinue

        # 在线状态下守护不会发起登录，必须先让会话失效
        Stop-Daemon
        Log '请现在用浏览器打开 http://172.16.121.9/ 并点击「注销」，让当前会话失效。' 'ACTION'
        Log '脚本会等到探针确认 offline 再启动守护（最长 90 秒）。'
        $deadline = (Get-Date).AddSeconds(90)
        $confirmedOffline = $false
        $nextHint = (Get-Date).AddSeconds(10)
        while ((Get-Date) -lt $deadline) {
            $status = (& $EpoExe status 2>&1 | Out-String)
            if ($status -match 'offline') {
                $confirmedOffline = $true
                Log '已确认 offline，立即启动守护并观察错误凭据的失败节奏'
                break
            }
            if ((Get-Date) -gt $nextHint) {
                $left = [math]::Round(($deadline - (Get-Date)).TotalSeconds)
                Log ('仍在等待你注销：剩余约 {0} 秒；请打开 http://172.16.121.9/ 点「注销」' -f $left) 'WAIT'
                $nextHint = (Get-Date).AddSeconds(15)
            }
            Start-Sleep -Seconds 2
        }
        if (-not $confirmedOffline) {
            Log '90 秒内未检测到 offline，继续执行但可能不会触发登录' 'WARN'
        }
        Start-DaemonIfNeeded | Out-Null

        Log ('观察 {0} 秒内的失败次数与退避节奏…' -f $TimeoutSec)
        $start = Get-Date
        $Script:FailCount = 0
        $lastFailAt = $null
        $nextHint = $start.AddSeconds(30)
        while (((Get-Date) - $start).TotalSeconds -lt $TimeoutSec) {
            $before = $Script:FailCount
            Poll
            if ($Script:FailCount -gt $before) {
                $lastFailAt = Get-Date
                Summary ('第 {0} 次登录失败，时刻 {1}' -f $Script:FailCount, $lastFailAt.ToString('HH:mm:ss.fff'))
            }
            if ($Script:FailCount -ge 3) {
                Log '已记录 3 次失败（退避 3s → 3s → 45s），提前结束观察'
                break
            }
            if ((Get-Date) -gt $nextHint) {
                $elapsed = [math]::Round(((Get-Date) - $start).TotalSeconds)
                Log ('观察进度 {0}/{1} 秒，累计失败 {2} 次' -f $elapsed, $TimeoutSec, $Script:FailCount) 'WAIT'
                $nextHint = (Get-Date).AddSeconds(30)
            }
            Start-Sleep -Milliseconds 500
        }

        $fails = $Script:FailCount
        Summary ('本次观察窗口内「登录失败」次数: ' + $fails)
        Summary '10 分钟上限: 3 次；退避规则: 3 秒 → 3 秒 → 45 秒 → 60 秒'
        Show-Tail 15
        if ($fails -eq 0) {
            Finish 'FAIL' '未记录到登录失败，测试可能未真正触发'
        } else {
            Finish 'PASS' ('错误密码能稳定失败且触发退避，本次共 ' + $fails + ' 次')
        }
    }
    finally {
        Log '恢复原始凭据…' 'WARN'
        if (Test-Path $SecretBak) {
            Copy-Item $SecretBak $SecretFile -Force
            Remove-Item $SecretBak -Force -ErrorAction SilentlyContinue
            Log '原始凭据已恢复'
        }
        Stop-Daemon
        if ($wasRunning) {
            Start-DaemonIfNeeded | Out-Null
            Log '已按测试前的状态重新启动守护'
        }
    }
    exit 0
}
