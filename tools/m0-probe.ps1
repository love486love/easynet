# epo M0 probe - run while connected to JXAU-Stu but NOT authenticated.
# Requires no internet. Writes a UTF-8 report next to this script.

$ErrorActionPreference = 'Continue'
$outDir  = Join-Path (Split-Path -Parent $PSCommandPath) '..\docs'
if (-not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir -Force | Out-Null }
$stamp   = Get-Date -Format 'yyyyMMdd-HHmmss'
$outFile = (Resolve-Path $outDir).Path + "\m0-raw-$stamp.txt"

$script:lastStatus   = $null
$script:lastLocation = $null
$script:lastBody     = ''

function W([string]$s) { $s | Out-File -FilePath $outFile -Encoding UTF8 -Append }

function Get-Body([Net.HttpWebResponse]$resp, [int]$limit) {
    $buf = New-Object byte[] $limit
    $stream = $resp.GetResponseStream()
    $n = $stream.Read($buf, 0, $limit)
    $stream.Close()
    if ($n -le 0) { return '' }
    $bytes = New-Object byte[] $n
    [Array]::Copy($buf, $bytes, $n)
    return [Text.Encoding]::UTF8.GetString($bytes)
}

function Probe([string]$label, [string]$url) {
    W ''
    W "=== $label ==="
    W "URL: $url"
    $resp = $null
    try {
        $req = [Net.HttpWebRequest]::Create($url)
        $req.AllowAutoRedirect = $false
        $req.Timeout = 6000
        $req.ReadWriteTimeout = 6000
        $req.UserAgent = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/155.0.0.0 Safari/537.36'
        $req.Referer = 'http://172.16.121.9/'
        try { $req.AddRange(0, 1023) } catch {}
        try { $resp = $req.GetResponse() } catch [Net.WebException] { $resp = $_.Exception.Response }
    } catch {
        W "ERR: $($_.Exception.Message)"
        return
    }
    if (-not $resp) { W 'RESULT: no response object'; return }

    $status = 0
    try { $status = [int]$resp.StatusCode } catch {}
    Write-Host ("  {0,-34} -> {1}" -f $label, $status)

    W "STATUS: $status"
    W 'HEADERS:'
    foreach ($k in $resp.Headers.AllKeys) { W "  $k`: $($resp.Headers[$k])" }

    $body = Get-Body $resp 1024
    W "BODY-LEN(<=1024): $($body.Length)"
    $squashed = ($body -replace "`r", ' ' -replace "`n", ' ')
    if ($squashed.Length -gt 400) { $squashed = $squashed.Substring(0, 400) }
    W "BODY-FIRST-400: $squashed"

    $script:lastStatus   = $status
    $script:lastLocation = $resp.Headers['Location']
    $script:lastBody     = $body
    try { $resp.Close() } catch {}
}

W '========================================='
W ' epo M0 probe report'
W " time: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"
W ' target: JXAU-Stu, expected state = NOT authenticated'
W '========================================='

W ''
W '=== [1] WLAN interface state ==='
try { (netsh wlan show interfaces | Out-String) -split "`r`n" | ForEach-Object { W $_ } }
catch { W "ERR: $($_.Exception.Message)" }

W ''
W '=== [2] IP / gateway / gateway MAC ==='
try {
    $cfg = Get-NetIPConfiguration -ErrorAction Stop | Where-Object { $_.InterfaceAlias -match 'WLAN|Wi-Fi' } | Select-Object -First 1
    $ip  = $cfg.IPv4Address.IPAddress
    $gw  = $cfg.IPv4DefaultGateway.NextHop
    W "IPv4: $ip"
    W "Gateway: $gw"
    if ($gw) {
        $nb = Get-NetNeighbor -IPAddress $gw -ErrorAction SilentlyContinue
        W "Gateway-MAC: $($nb.LinkLayerAddress)"
    }
} catch { W "ERR: $($_.Exception.Message)" }

W ''
W '=== [3] Chkstatus (gateway status endpoint) ==='
Probe 'chkstatus' 'http://172.16.121.9/drcom/chkstatus?callback=dr1002&jsVersion=4.X&lang=zh&v=10000'

W ''
W '=== [4] Portal page (a79) ==='
Probe 'a79' 'http://172.16.121.9/a79.htm'

$location = $script:lastLocation
$body     = $script:lastBody
if ($location -and $location -match 'userip=') {
    W ''
    W '=== [5] Params parsed from Location ==='
    W "Location: $location"
    foreach ($p in 'userip', 'wlanacip', 'wlanacname', 'usermac') {
        $m = [regex]::Match($location, "$p=([^&]*)")
        W "$p = $($m.Groups[1].Value)"
    }
} else {
    W ''
    W '=== [5] No redirect params; params seen in page script ==='
    foreach ($p in 'v46ip', 'v4serip', 'ss5', 'ss6', 'ss3', 'ss4') {
        $m = [regex]::Match($body, "$p\s*=\s*'([^']*)'")
        if ($m.Success) { W "$p = $($m.Groups[1].Value)" }
    }
}

W ''
W '=== [6] Probe: school homepage (primary, HTTP) ==='
$rand = Get-Random -Minimum 100000 -Maximum 999999
Probe 'jxau-home' "http://www.jxau.edu.cn/?epo=$rand"

W ''
W '=== [7] Probe: baidu (backup, HTTP) ==='
Probe 'baidu' "http://www.baidu.com/?epo=$rand"

W ''
W '=== [8] Probe: generate_204 (expected to be special-cased) ==='
Probe 'generate_204' 'http://captive.apple.com/generate_204'

W ''
W '=== [9] Login attempt with FAKE account (does not touch real account) ==='
$ip = $null; $ac = $null; $um = $null
if ($location) {
    $m = [regex]::Match($location, 'userip=([^&]*)');   if ($m.Success) { $ip = $m.Groups[1].Value }
    $m = [regex]::Match($location, 'wlanacip=([^&]*)'); if ($m.Success) { $ac = $m.Groups[1].Value }
    $m = [regex]::Match($location, 'usermac=([^&]*)');  if ($m.Success) { $um = $m.Groups[1].Value }
}
if (-not $ip) { try { $ip = $cfg.IPv4Address.IPAddress } catch {} }
if (-not $ac) { $ac = '172.16.121.9' }
W "using ip=$ip ac=$ac usermac=$um"

$loginUrl = 'http://172.16.121.9:801/eportal/portal/login?callback=dr1003&login_method=1' +
            '&user_account=%2C0%2C00000000000%40cmcc&user_password=epo-test-invalid' +
            "&wlan_user_ip=$ip&wlan_user_ipv6=&wlan_user_mac=$um&wlan_ac_ip=$ac&wlan_ac_name=" +
            "&jsVersion=4.2.1&terminal_type=1&lang=zh-cn&v=$rand"
Probe 'login-fake' $loginUrl

W ''
W '=== [10] Same login without any params (control test) ==='
$loginUrl2 = "http://172.16.121.9:801/eportal/portal/login?callback=dr1003&login_method=1&user_account=%2C0%2C00000000000%40cmcc&user_password=epo-test-invalid&jsVersion=4.2.1&terminal_type=1&lang=zh-cn&v=$rand"
Probe 'login-no-params' $loginUrl2

W ''
W '=== [11] Legacy form endpoint (compat branch check) ==='
Probe 'legacy-login' "http://172.16.121.9:801/eportal/?c=ACSetting&a=Login&url=drappall"

W ''
W '========================================='
W ' END'
W " report  -> $outFile"
W '========================================='

Write-Host ''
Write-Host 'Done. Report saved to:'
Write-Host $outFile
