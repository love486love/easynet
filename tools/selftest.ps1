# 校园网自动恢复自测：录入凭据 → 启动守护 → 你手动注销 → 观察是否自动登回
[CmdletBinding()]
param(
    [string]$Epo = "",
    [int]$TimeoutSec = 180,
    [switch]$SkipCredPrompt
)

$ErrorActionPreference = 'Stop'

function Info($m) { Write-Host "[*] $m" -ForegroundColor Cyan }
function Ok($m) { Write-Host "[+] $m" -ForegroundColor Green }
function Warn($m) { Write-Host "[!] $m" -ForegroundColor Yellow }

$proc = $null
$transcriptPath = Join-Path $PSScriptRoot 'selftest-last.txt'
try {
    Start-Transcript -Path $transcriptPath -Force | Out-Null
} catch {
    Write-Host "[!] 无法写入运行记录：$transcriptPath" -ForegroundColor Yellow
}

try {
    # 参数默认值里取不到 $PSScriptRoot，必须在脚本体内解析
    if ([string]::IsNullOrWhiteSpace($Epo)) {
        $root = $PSScriptRoot
        if ([string]::IsNullOrWhiteSpace($root)) {
            $root = Split-Path -Parent $MyInvocation.MyCommand.Path
        }
        $Epo = Join-Path $root '..\build\epo.exe'
    }

    if (-not (Test-Path $Epo)) {
        throw "未找到 $Epo`n请先构建：go build -o build\epo.exe ./cmd/epo"
    }

    $dir = Join-Path $env:LOCALAPPDATA 'epo'
    $secret = Join-Path $dir 'secret.bin'

    # 1. 凭据：没有就现场录入，密码不出现在屏幕与命令行
    if (-not (Test-Path $secret)) {
        if ($SkipCredPrompt) { throw "缺少加密凭据，且指定了 -SkipCredPrompt" }

        Info "首次运行，需要录入校园网凭据（密码不回显，也不会写入日志）"
        $user = Read-Host "学号"
        $secure = Read-Host "密码" -AsSecureString
        $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
            [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))

        $carrier = Read-Host "运营商 cmcc/ctcc/cucc/campus（回车=cmcc）"
        if ([string]::IsNullOrWhiteSpace($carrier)) { $carrier = 'cmcc' }

        # 临时文件放在 build 下（已 gitignore），不用 %TEMP%，避免明文落到工作区外
        $tmp = Join-Path (Split-Path -Parent $Epo) 'epo-cred.tmp.json'
        $payload = [ordered]@{ username = $user; password = $plain; carrier = $carrier } | ConvertTo-Json
        [IO.File]::WriteAllText($tmp, $payload, [Text.UTF8Encoding]::new($false))

        & $Epo save-cred -cred $tmp 2>&1 | ForEach-Object { "    $_" }
        $saveCode = $LASTEXITCODE
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
        Remove-Variable plain

        if ($saveCode -ne 0) { throw "保存凭据失败（退出码 $saveCode）" }
        if (-not (Test-Path $secret)) { throw "凭据文件未生成：$secret" }

        Ok "凭据已用 DPAPI 加密保存到 $secret"
    } else {
        Ok "已存在加密凭据，跳过录入"
    }

    # 2. 记录起始状态
    Info "检查当前状态…"
    $before = (& $Epo status 2>&1 | Out-String).Trim()
    Info "当前：$before"
    if ($before -notmatch 'online') {
        Warn "当前不在线，守护会先自己登录一次，之后再做掉线测试"
    }

    # 3. 后台启动守护
    $logFile = Join-Path $dir 'logs\epo.log'
    Info "启动守护进程…"
    $proc = Start-Process -FilePath $Epo -ArgumentList 'daemon' -PassThru -WindowStyle Hidden
    Start-Sleep -Seconds 3

    if ($proc.HasExited) {
        Warn "守护启动后立即退出（退出码 $($proc.ExitCode)）"
        if (Test-Path $logFile) {
            Get-Content $logFile -Tail 5 | ForEach-Object { "    $_" }
        } else {
            "    （未生成日志，说明在创建日志前就失败了）"
        }
        return
    }
    Ok "守护已启动（PID $($proc.Id)）"

    Write-Host ""
    Write-Host "现在请用浏览器打开 http://172.16.121.9/ ，点击「注销」。" -ForegroundColor Yellow
    Write-Host "脚本会自动观察「离线 → 恢复在线」，最长等待 $TimeoutSec 秒。" -ForegroundColor Yellow
    Write-Host ""

    # 4. 轮询状态，等待完整的掉线→恢复过程
    $start = Get-Date
    $sawOffline = $false
    $recoverSeconds = $null

    while (((Get-Date) - $start).TotalSeconds -lt $TimeoutSec) {
        Start-Sleep -Seconds 2
        $now = (& $Epo status 2>&1 | Out-String)
        if ($now -match 'offline') { $sawOffline = $true }
        if ($sawOffline -and $now -match 'online') {
            $recoverSeconds = [math]::Round(((Get-Date) - $start).TotalSeconds, 1)
            break
        }
    }

    if ($null -ne $recoverSeconds) {
        Ok "已自动恢复，总耗时约 $recoverSeconds 秒（含脚本 2 秒轮询误差）"
    } elseif ($sawOffline) {
        Warn "检测到了掉线，但在 $TimeoutSec 秒内没有恢复，请检查日志"
    } else {
        Warn "未检测到掉线：可能你还没点注销，或网关没有立即断网"
    }

    # 5. 输出证据
    Info "最近日志（$logFile）："
    if (Test-Path $logFile) {
        Get-Content $logFile -Tail 12 | ForEach-Object { "    $_" }
    } else {
        "    （暂无日志）"
    }

    $trustFile = Join-Path $dir 'trusted_macs.json'
    if (Test-Path $trustFile) {
        Ok "TOFU 信任文件："
        Get-Content $trustFile | ForEach-Object { "    $_" }
    } else {
        Warn "尚未生成 TOFU 信任文件（本次可能没有触发登录）"
    }
}
finally {
    if ($null -ne $proc -and -not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force
        Ok "守护已停止"
    }
    try { Stop-Transcript | Out-Null } catch { }
}
