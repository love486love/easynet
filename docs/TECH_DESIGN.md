# easynet 技术设计

| 项目 | 内容 |
|---|---|
| 项目代号 | easynet（原名 epo，ePortal Auto Login） |
| 平台范围 | Windows 10 / 11 x64 |
| 适用网络 | 江西农业大学 `JXAU-Stu`（单校专用） |
| 技术栈 | Go 1.22 + Wails v2.10.2 + 手写 HTML/CSS/JS（无 Node 构建） |
| 分发 | GitHub Release（绿色版 zip + Inno Setup 安装包） |

---

## 1. 范围

- 单一 SSID（`JXAU-Stu`）、单一 profile
- Dr.COM ePortal 单一协议实现
- 单个 `easynet.exe`：默认打开界面，带 `--daemon` 参数时无窗常驻 + 系统托盘
- 自动登录、断线重连、开机自启、多账号管理

非目标：多校 / 多门户 / macOS / Linux / ARM64 / 多设备共享 / 绕过计费 / 遥测 / 自动更新。

---

## 2. 协议事实

以下均为真机抓包与门户返回验证过的结论（详见 [M0 实测报告](M0-REPORT.md)）。

| 项 | 值 |
|---|---|
| 认证服务器 | `172.16.121.9:801`（与默认网关不同网段） |
| 登录接口 | `GET /eportal/portal/login`，JSONP 返回 `dr1003({...})` |
| 账号字段 | `user_account`，格式 `,0,<学号>@<运营商>`；移动为 `@cmcc` |
| 密码字段 | `user_password`（明文 query，这是校园网关的限制） |
| 成功判定 | `{"result":1,"msg":"Portal协议认证成功！"}` |
| 失败判定 | `{"result":0,"msg":"账号不存在" / "密码错误…", "ret_code":1}` |
| 状态查询接口 | `chkstatus` 在线/离线都返回 `result:0`，**不能用于判定** |
| 附加参数 | `jsVersion=4.2.1`、`terminal_type=1`、`login_method=1`、`lang=zh-cn` |
| 运营商后缀 | `cmcc` → `cmcc`，`ctcc` → `telecom`，`cucc` → `unicom`，校园线路 → 空后缀 |

未认证时，任意 HTTP 请求会被 302 到 `a79.htm`，跳转参数里带 `userip` / `wlanacip` / `usermac`，登录时原样回传。

---

## 3. 在线判定

探针只读响应头与前 1024 字节（`Range: bytes=0-1023`），单次流量 ≤ 10 KB。

配置了两个探针：

| 探针 | 正常状态必须出现的正文片段 |
|---|---|
| `http://www.jxau.edu.cn/?easynet=<随机>` | `jxau.edu.cn` |
| `http://www.baidu.com/?easynet=<随机>` | `baidu.com` |

判定规则（`internal/probe/judge.go`，纯函数，可用真实样本回归）：

1. **2xx（含 206）**：配置了 `expect` 时必须命中正文才算在线——防住「网关注入登录页却返回 200」的透明劫持；未配置 `expect` 时退回拦截页特征排除法
2. **3xx**：`Location` 指向探针本身或允许的站点 → 在线；跳到门户或其它主机 → 离线
3. **5xx**：返回「未知」，交给另一个探针决定
4. 其余状态码 → 离线

拦截页兜底特征：`WISPAccessGatewayParam`、`MessageType>110`、`DDDDD`、`upass`、`Dr.COMWebLogin`、`ACSetting`、`<input type="password"`。

### 3.1 自动连接 Wi-Fi（可选，默认关闭）

开关状态由 `%LOCALAPPDATA%\easynet\wifi_auto` 标记持久化（存在即开启）。

开启后，守护每轮探测前检查当前 SSID：

- 已经是 `JXAU-Stu`：什么都不做
- 不是：调用 `netsh wlan connect name=<SSID> ssid=<SSID>`
- 系统里没有该网络的 profile：先按开放网络（无加密、无 802.1X）创建一份再连
- 连接失败后 1 分钟内不重复尝试，避免 Wi-Fi 硬件关闭时刷日志

软件不会开启或关闭无线网卡，也不修改系统电源设置；Wi-Fi 被硬件开关关闭时只记录日志。

---

## 4. 进程模型

```
┌──────────────────────────────────────┐
│ easynet.exe --daemon（常驻）          │  ← 开机自启指向这条命令
│  · 状态机 / 退避 / 单实例             │
│  · 系统托盘（菜单可拉起界面）          │
└──────────┬───────────────────────────┘
           │ %LOCALAPPDATA%\easynet\state.json（状态快照）
           │ %LOCALAPPDATA%\easynet\stop.flag（停止请求）
     ┌─────┴──────────┐
     ▼                ▼
┌──────────┐   ┌───────────────────┐
│ 托盘菜单  │   │ easynet.exe       │
│ 打开界面  │   │ 无参数，按需启动/退出 │
└──────────┘   └───────────────────┘
```

规则：

1. **守护是唯一的认证执行者**，独立进程常驻并持有托盘；界面关闭后继续工作
2. **界面按需启动**：托盘「打开界面」或用户双击；关闭窗口即进程退出
3. 界面通过 `state.json` 判断守护是否在跑（20 秒无更新视为未运行），不做进程探测
4. 界面「守护」开关默认开启，状态由 `daemon_off` 标记持久化（标记不存在即视为开启）；打开时执行 `exec easynet.exe --daemon`
5. 关闭开关写入 `daemon_off` 并发 `stop.flag`，守护在 200ms 轮询中读到后优雅退出，不做跨进程强杀
6. 界面启动时自动补上守护（开关开启且已保存账号时）；关掉界面或从托盘退出都不改变开关状态
7. 单实例：界面用 `Local\easynet-gui`，守护用 `Local\easynet-daemon`。界面冲突时把已有窗口置前；守护冲突时静默退出
8. 托盘「退出」写 `stop.flag` 并给界面窗口发 `WM_CLOSE`，两个进程一起收尾

---

## 5. 守护状态机

节奏由 `PolicyConfig` 驱动，依赖以函数注入，用假时钟做单测：

| 场景 | 行为 |
|---|---|
| 在线 | 每 5 秒探测一次 |
| 检测到掉线 | 立即登录 |
| 登录失败 | 每 3 秒快速重试 |
| 连续失败 3 次 | 退避升到 45 秒 |
| 连续失败 5 次以上 | 封顶 60 秒 |
| 10 分钟内登录次数 | 最多 3 次 |
| 登录成功 | 500ms 间隔复核 2 次，确认在线后记录网关 MAC |
| 会话存活短于 60 秒 | 直接进入长退避，避免反复打扰门户 |

守护每 5 秒刷新 `state.json` 心跳，长退避期间界面也不会误判「未运行」。

---

## 6. 安全与校验

**凭据**：`%LOCALAPPDATA%\easynet\accounts.bin`，用 Windows DPAPI（`CryptProtectData`）以当前用户身份加密，换账户或换电脑都无法解密。支持多账号，`current` 字段指向当前使用的账号。

**四项校验**（`internal/guard`）：

| 校验项 | 处理 |
|---|---|
| 目标 SSID 是否与当前一致 | 不一致则拒绝登录 |
| 门户主机是否在白名单 | 不在则拒绝 |
| 网卡是否启用随机 MAC | 只告警，不阻断 |
| 默认网关 MAC 是否在信任列表 | 在则放行；列表为空时只记录不阻断 |

**TOFU**：首次登录成功且复核在线后，用 `SendARP` 记录默认网关 MAC，LRU 最多保存 8 个。网关 MAC 变化时拒绝自动登录，需要用户手动确认。

---

## 7. 数据与日志

| 位置 | 内容 |
|---|---|
| `%LOCALAPPDATA%\easynet\accounts.bin` | DPAPI 加密的多账号与当前账号 |
| `%LOCALAPPDATA%\easynet\state.json` | 守护状态快照（state / message / pid / updated_at） |
| `%LOCALAPPDATA%\easynet\stop.flag` | 停止请求 |
| `%LOCALAPPDATA%\easynet\daemon_off` | 守护开关关闭标记（不存在即开启） |
| `%LOCALAPPDATA%\easynet\wifi_auto` | 自动连接校园 Wi-Fi 开关标记（存在即开启，默认关闭） |
| `%LOCALAPPDATA%\easynet\tutorial_done` | 新手引导已播放标记 |
| `<程序目录>\log\easynet.log` | 运行日志，5 MB 轮转、保留 3 份；程序目录不可写时回退到 `%LOCALAPPDATA%\easynet\logs` |

界面「运行日志」页支持导出日志与直接打开日志目录。

---

## 8. 构建与发布

**本地构建**（Windows，Go 1.22，不需要 Node/npm）：

```powershell
go test ./...
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
cd cmd/easynet
wails build -platform windows/amd64 -webview2 browser -ldflags "-s -w"
```

产物：`cmd/easynet/build/bin/easynet.exe`（约 9.7 MB）。

**自动发布**（`.github/workflows/release.yml`，推送 `v*` tag 触发）：

| 步骤 | 内容 |
|---|---|
| 测试 | `go test ./...` |
| 构建 | `easynet.exe`（wails v2.10.2，无 Node） |
| 打包 | `easynet-windows-x64.zip`：exe + README + uninstall.bat + checksums |
| 安装包 | Inno Setup 6，中文语言包，输出 `dist-installer/easynet-setup-*.exe` |
| 发布 | zip、zip 的 SHA256、安装包 |
| 权限 | `contents: write` |

---

## 9. 已知限制

- 仅 Windows 10/11 x64，需要系统 WebView2
- 无代码签名，SmartScreen 会提示「未知发布者」
- 无自动更新，升级需手动覆盖
- 凭据在网关侧以明文 HTTP 传输，这是学校网关的设计限制
- 学校若开启验证码、无感知认证或 MAC 白名单，本工具可能失效

---

## 10. 参考

- [M0 实测报告](M0-REPORT.md)：抓包时间线、探针响应、失败文案
- [发布说明](RELEASE_NOTES.md)
