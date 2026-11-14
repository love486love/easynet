# easynet 开发手册

> 面向开发者的环境、构建、测试与排障速查。流程规范见 [DEV_PROCESS.md](DEV_PROCESS.md)（方案先批、测试清单、文案先审），协议细节见 [TECH_DESIGN.md](TECH_DESIGN.md)。

## 1. 开发环境（Windows）

| 项 | 值 |
|---|---|
| Go | `d:\trae工作区\.tools\go\bin`（PATH 前置） |
| Wails CLI | `d:\trae工作区\.tools\gopath\bin\wails.exe`（GOPATH 也指这里） |
| GOTMPDIR | **必须** `$env:GOTMPDIR = "d:\trae工作区\.tmp"` |
| GOCACHE | **必须** `$env:GOCACHE = "d:\trae工作区\.gocache"` |
| GOPROXY | `https://goproxy.cn,direct`（校园网直连 proxy.golang.org 会超时） |

> GOTMPDIR/GOCACHE 不设置会落在 C 盘 Temp，被 360 清除导致 `fork/exec: Access is denied`。

## 2. 常用命令

```powershell
# 环境（每条命令前置）
$env:PATH = "d:\trae工作区\.tools\gopath\bin;d:\trae工作区\.tools\go\bin;" + $env:PATH
$env:GOTMPDIR = "d:\trae工作区\.tmp"; $env:GOCACHE = "d:\trae工作区\.gocache"
$env:GOPATH = "d:\trae工作区\.tools\gopath"; $env:GOPROXY = "https://goproxy.cn,direct"

cd d:\trae工作区\epo
gofmt -l .                 # CI 会挡未格式化文件；-w 修复
go vet ./...
go test ./...              # 13 包，全绿才可提交
cd cmd\easynet
wails build                # 产物：build\bin\easynet.exe（cwd 必须是 cmd/easynet）
```

发布：`git tag vX.Y.Z && git push origin main && git push origin vX.Y.Z` → tag 触发 `release.yml`（构建+安装包+5 资产），push 触发 `ci.yml`（gofmt+test+build）。用 `gh run list` / `gh run watch <id>` 盯进度。

## 3. 目录结构

```
epo/
├── cmd/easynet/            # 主程序：GUI 绑定、更新器、守护启动
│   ├── app.go              #   Wails API（Status/Login/Accounts/Campus/SetCampus...）
│   ├── update.go           #   检查更新/下载/替换（probeSwap 预检 + helper 脚本）
│   ├── daemon.go           #   RunDaemon：状态机 + 登录链路装配
│   └── frontend/dist/      #   零构建前端：index.html + style.css + app.js（go:embed）
├── internal/
│   ├── portal/drcom/       # 东区 Dr.COM dr1003 协议
│   ├── portal/ruijie/      # 本部锐捷协议 + 验证码 OCR（captcha.go，testdata/ 4 真图）
│   ├── probe/              # 探针（不跟随重定向；劫持页 body 跳转提取 hijackRedirect）
│   ├── session/            # ProbeOnce + EndpointFromEvidence（Location→门户参数）
│   ├── daemon/             # 守护状态机（判离线/退避/配额），纯逻辑可单测
│   ├── guard/              # 登录前校验（SSID/门户白名单/TOFU MAC）
│   ├── config/             # Profile（双校区/多SSID/优先校区），profile.json 覆盖
│   ├── creds/              # DPAPI 多账号（分校区 Currents，旧数据 normalize）
│   ├── netinfo/            # 网卡信息（GetIfEntry2 计数器 + netsh）
│   ├── trust|autostart|state|logx
└── packaging/easynet.iss   # Inno Setup（版本由 CI 注入）
```

## 4. 关键机制速查

- **双校区选型**：探测拿 AC 重定向 → `indexpath` 含 `index.jsp` = 本部锐捷；Location 空 → 按当前 SSID 兜底（JXAU* → 锐捷）。本部拦截是 **200 + 511B JS 跳转页**（不是 302），`probe.hijackRedirect` 从 body 提取。
- **本部登录**：GET index.jsp 建 session → GET validcode → OCR（模板匹配，内置 4 图 + `%LOCALAPPDATA%\easynet\captcha\` 自举；识别不出换图绝不猜）→ POST login（双重 URL 编码）→ success.jsp。失败分类：超时/验证码/Code30 换图重试，密码错直接返回。
- **判离线**：目标 SSID（JXAU-Stu/JXAU/CMCC-JXAU）连续 3 次探针失败即登录；其他 SSID 不登录；SSID 读不到走 6 次兜底。链路类错误不计配额。
- **自动更新**：probeSwap 预检（失败=旧文件未动，直接报拦截）→ selfupdate.Apply → Access denied 立即停 → 生成 `update-helper.bat`。绿色版原地替换 / 安装版静默安装。

## 5. 环境坑（血泪清单）

| 坑 | 处理 |
|---|---|
| 360 拦进程内 exe 改名（rename/copy 全拒，手动改名弹窗允许） | 更新器已做预检+提示+helper 脚本；开发时改 `D:\ops测试` 内文件需用户手动操作 |
| PS5 脚本中文乱码崩 | .ps1 必须 **UTF-8 with BOM**（`[IO.File]::WriteAllText($p,$c,(New-Object Text.UTF8Encoding($true)))`） |
| PS5 `ConvertFrom-Json` 传 byte[] 会逐元素解析并可毁文件 | 必须先转字符串再传参：`ConvertFrom-Json ([Text.Encoding]::UTF8.GetString($bytes))` |
| PS5 `$i:` 变量冒号被解析成驱动器 | 用 `${i}:` |
| PS5 单引号串内 `''` 才是字面引号；含双引号表达式易被拆参数 | 复杂表达式改双引号串或拆变量 |
| `gh release view --jq` 表达式带双引号在 PS5 拆参 | 去掉 --jq 用默认表格输出 |
| 校园网 github.com:443 时断时续 | push 前 `Test-NetConnection github.com -Port 443`；api.github.com 通常可用 |
| gofmt 不过挡 CI | 提交前必跑 `gofmt -l .` |
| 凭据文件绝不入库 | `.gitignore` 已挡 `*.har` `*.log`；HAR/日志/探针报告在仓库目录时**不要 git add** |
| 中文验证 UTF-8 检查 exe 内嵌 | 用 `[Text.Encoding]::UTF8.GetString`（ASCII 解码中文不可靠） |

## 6. 真机测试工具

- `d:\trae工作区\benbu-probe.bat`：本部拦截行为实测（DNS/302/劫持 body/门户探活），断网窗口本地可跑，报告写 `benbu-probe-report.txt`
- `d:\trae工作区\benbu-probe2.bat`：劫持页 body 抓取 + 用测试账号完整 login 实测（凭据从 accounts.bin 读，不硬编码）
- 验证码 fixture：`internal/portal/ruijie/testdata/validcode-{1..4}.png`（答案 9456/2284/0336/9348，测试里对齐）
- E2E 判读：日志关键词 = `协议 ruijie-eportal` → `登录成功，进入在线态` → `已记录网关 MAC`

## 7. 发布检查表

1. `gofmt -l .` 空 + `go vet ./...` + `go test ./...` 全绿
2. 版本号 9 处同步：`version.go`、`wails.json`、`easynet.iss`（注释+define）、`index.html`（verBadge+about-ver）、`README`（当前版本行+roadmap）、`RELEASE_NOTES.md`（重写）
3. 公众文案交用户审阅
4. commit（指定文件，排除日志/HAR/报告）→ `git tag vX.Y.Z` → push main + tag
5. `gh run list` 盯 release+ci 双绿 → `gh release view` 核对 5 资产（exe/setup/zip/sha256/checksums）
