# epo 技术设计文档

| 项目 | 内容 |
|---|---|
| 文档版本 | **v0.8**（第三轮评审整改 + 在线态实测） |
| 项目代号 | epo（ePortal Auto Login） |
| 平台范围 | **Windows 10 / 11 x64** |
| 适用网络 | **江西农业大学 JXAU-Stu**（单校专用） |
| 技术栈 | Go 1.21+ / Wails v2 / Vue3 / 系统托盘 |
| 分发 | GitHub Release + Gitee 镜像 |
| 许可证 | MIT |

## 变更记录

| 版本 | 变更 |
|---|---|
| v0.1 | 三平台 + 多门户 + 自动发现（初版） |
| v0.2 | 收缩为 Windows + 江农专用；修正探针地址；补 IPC、降级路径、ADR |
| **v0.3** | 按评审意见整改：判定算法三信号化、主备双探针、凭据发送前三重校验、daemon 独立进程化、错误码合并、端点抽象去 drcom 化、目录改 `LOCALAPPDATA`、CI 补齐、指标校准 |
| **v0.4** | **按真实抓包修正协议**：登录实为 `GET /eportal/portal/login`（JSONP），成功判定为 `result:1`，运营商为 `@cmcc`，账号格式 `,0,<学号>@后缀`；删除 `R1..R7`、`DDDDD/upass` 等未使用方法；新增 `chkstatus` 状态接口 |
| **v0.5** | 第二轮评审整改：**修正 3xx 判定方向**、凭据校验并集改交集、拆分并删除不可实现的 MAC 比对、补登录前置流程、明确 daemon 拉起与退出语义、"已在线"兜底、参数进 `extra_params`、CI 修复 |
| **v0.6** | **托盘从 GUI 迁入 daemon**（空闲态常驻回到 20–40MB）；GUI 改为按需启动、关闭即退出；新增**默认网关 MAC 首次信任（TOFU）**；**探针改为只读响应头**（单次 <5KB、全天 <10MB）；安全边界如实声明；补齐体积与流量预算 |
| **v0.7** | **M0 实测完成**：确认未认证态三探针均返回带参 302、判定规则定稿；取得失败响应样本（含 `ret_code`）；确认旧版表单接口已废弃并删除兼容分支；确认 `chkstatus` 无法用于判定；网关 MAC 可读取 |
| **v0.8** | 第三轮评审整改：**补在线态样本并修正 206 判定**（`Range` 导致 206 被误判为未认证）；3xx 改按"同站"比较；`wlan_user_ip` 改取 `Location.userip`；TOFU 防投毒 + 一键信任 + LRU；**二进制拆分为 CLI / daemon / GUI**（消除子系统冲突与托盘 cgo 风险）；指标拆口径；CI 补 `permissions` |

## 评审整改对照

| 编号 | 评审问题 | 本文档处置 |
|---|---|---|
| P0-1 | 判定表缺「200 直出登录页」与「3xx 指向公网/HTTPS」 | §6.1 改为三信号合判 + 未定义分支 + 连续 2 次确认 |
| P0-2 | 探针仍为待验证项 | §6.1 改主备双探针，离线行为升级为 M0 阻塞 |
| P0-3 | 缺 SSID/网关绑定 | §6.2 新增凭据发送前三重校验（**规则已按江农网段事实修正**） |
| P0-4 | 错误码 2/3/4 无证据 | §11 合并为「认证被拒 + 原始摘要」，细分留待证据 |
| P1-1 | daemon 内嵌 GUI | §3.1 改为 daemon 独立常驻，GUI 仅作前端 |
| P1-2 | AUTH_FAILED 无出口 | §5.3 增加复位规则 |
| P1-3 | 退避振铃 | §5.3 增加全局频率上限与短存活检测 |
| P1-4 | ADR 前提失效 | §18 重写 ADR-001 / ADR-002 论证 |
| P1-5 | Portal 假抽象 | §4 端点改为协议无关结构 |
| P1-6 | 分发与可持续性 | §9 增加 Gitee 镜像、无自动更新说明 |
| P2-1~8 | 实现坑 | 分发落 §12，CI 落 §9，竞态落 §3.1，其余落 §5 |
| P3-1~8 | 文档细节 | 交叉引用、状态机、M0 工具链、指标、去重均已修正 |

---

## 1. 背景与范围

### 1.1 问题

`JXAU-Stu` 为 Open 无加密无线网络，连上后需经 Dr.COM ePortal（`172.16.121.9:801`）网页认证才能访问外网。Windows 的"自动连接"只能关联热点，无法完成认证。官方哆点 PC 客户端的院校名单不含江西农大；学校侧无感知认证开关已确认关闭（`en_perceive=0`）。

### 1.2 本期范围

- Windows 10/11 x64
- 单一 SSID（`JXAU-Stu`）、单一 profile
- Dr.COM ePortal 单一协议实现
- 常驻 `epo-daemon.exe`（无窗 + 托盘）+ `epo.exe` CLI + `epo-gui.exe` 按需界面
- 自动登录、断线重连、开机自启

### 1.3 非目标

多校 / 多门户 / 插件体系 / macOS / Linux / ARM64 / 多设备共享 / 绕过计费 / 打包厂商客户端 / 遥测 / 自动更新。

---

## 2. 已验证事实

> 纪律：**标为"待验证"的项不得以推测值编码**，必须实测确认后写入。

| 项 | 结果 | 状态 |
|---|---|---|
| SSID 安全 | Open / Cipher None / 无 EAP | ✅ |
| MAC 随机化 | 已关闭 | ✅ |
| 本机 IP / 网关 | 网关固定 `10.100.0.1`；本机 IP 为 DHCP 动态（不同会话见过不同地址） | ✅ |
| 认证服务器 | `172.16.121.9:801`（**与网关不同网段**） | ✅ |
| 登录接口 | `GET /eportal/portal/login`（JSONP，端口 801） | ✅ 抓包验证 |
| 账号字段 | `user_account`，格式 `,0,<学号>@<运营商>`；移动 = `@cmcc` | ✅ 抓包验证 |
| 密码字段 | `user_password`（明文，URL query） | ✅ 抓包验证 |
| 成功判定 | `{"result":1,"msg":"Portal协议认证成功！"}` | ✅ 抓包验证 |
| 失败判定 | `{"result":0,"msg":"账号不存在","ret_code":1}` | ✅ M0 实测 |
| 状态查询接口 | `chkstatus` 在线/离线均 `result:0` → **不可用于判定** | ✅ 实测结论 |
| 旧版表单接口 | 返回 EPortal Vue 页面，**已废弃** | ✅ 实测，分支已删 |
| 页面编码 | 登录接口 UTF-8；`a79.htm` 为 GB2312/GBK（仅用于读页面） | ✅ |
| `wlanacip` 来源 | 认证跳转 URL 查询参数（实测 `172.16.x.x`） | ✅ |
| `wlanacname` | 空串 | ✅ |
| `usermac` 来源 | 跳转 URL 查询参数，为**不透明长串**（非裸 MAC），原样回传 | ✅ |
| 无感知认证 | `en_perceive = 0` | ✅ |
| 官方客户端下载页 | 依赖的 `pc_70.js` 返回 404 | ✅ |
| 系统级探测地址 | **离线正常 302**；在线返回 200 "Success"（CDN 边缘差异，非网关特判） | ✅ 双向实测 |
| 运营商参数 | 移动 = `@cmcc` | ✅ 抓包验证 |
| `R1..R7` / `loginMethod` | 请求中未出现，**不需要** | ✅ 抓包验证 |
| **未认证时的探针响应** | **302 → `Location: http://172.16.121.9/a79.htm?userip=…&wlanacip=172.16.x.x&usermac=…`**；Server `NetEngine Server 1.0` | ✅ M0 实测 |
| 拦截页正文特征 | 含 `WISPAccessGatewayParam` XML / `MessageType 110` | ✅ M0 实测 |
| 三个探针是否一致 | 学校主页 / 百度 / `generate_204` **三者均返回同一 302** | ✅ M0 实测 |
| 失败响应格式 | JSONP `dr1003({"result":0,"msg":"账号不存在","ret_code":1});` | ✅ M0 实测（假账号） |
| 旧版表单接口 | 返回 EPortal Vue 页面（HTML），**不是登录表单 → 不可用** | ✅ M0 实测，删除该分支 |
| 默认网关 MAC | 可读取（实测值已脱敏） | ✅ M0 实测，TOFU 可行 |
| `chkstatus` 语义 | 未认证时 `result:0 / hidn:-5`；在线样本同为 `result:0` → **无法用于判定** | ✅ 实测，维持旁路 |

---

## 3. 架构

### 3.1 进程模型

```
┌───────────────────────────────┐
│ epo-daemon.exe（常驻 20–40MB）│  ← 开机自启指向这里
│  · 状态机 / 退避 / 单实例       │
│  · 托盘图标（按需拉起 GUI）      │
└──────────┬────────────────────┘
           │ 命名管道 \\.\pipe\epo-daemon（JSON Lines）
     ┌─────┴──────────┐
     ▼                ▼
┌──────────┐   ┌───────────────┐
│ epo.exe  │   │ epo-gui.exe   │
│ CLI 薄壳 │   │ 按需启动，可退出 │
└──────────┘   └───────────────┘
```

规则：

1. **daemon 是唯一的认证执行者**，独立常驻，**同时持有系统托盘图标**（纯 Go 托盘库，Windows 下免 cgo），不随 GUI 退出而终止
2. **GUI 按需启动**：仅在托盘菜单「打开主界面」或用户双击 exe 时启动；**关闭窗口即进程退出**，不再"隐藏到托盘"——托盘已由 daemon 提供
3. GUI 启动时先探测管道；无 daemon 则 `exec` 拉起 `epo.exe daemon`，以"管道可连接"为就绪信号（不是定时轮询）
4. 管道存在但 daemon 未就绪时，GUI 显示"启动中"并重试；**此时 CLI 的 `login` 一律排队等待**，禁止旁路直接发请求（避免双进程同时登录）
5. CLI 的 `status/pause/resume/login` 全部走管道；`doctor` 为一次性独立命令，不需要 daemon
6. **`epo login` 在无 daemon 时**：拉起 daemon → 等待就绪 → 执行 → **保留 daemon 不退出**。与 GUI 共用同一段拉起逻辑，不重复实现
7. **daemon 进程创建参数**：`CREATE_NO_WINDOW | DETACHED_PROCESS`，不继承句柄，保证父进程退出不带走认证进程，也不留孤儿进程
8. **互斥体获取失败方静默退出**（说明已有实例在跑），不报错、不弹窗
9. 单实例：命名互斥体 `Local\epo-daemon`（单用户单会话设计，不支持 RDP 多用户并发）
10. daemon 的 goroutine 统一 `recover`，崩溃后重建状态而非拖垮进程

> 这样拆分的收益：**空闲态常驻只有 daemon + 托盘（20–40MB）**，250MB 的 WebView2 只在用户真正打开界面时存在。v0.5 把托盘放在 GUI 里，等于让 250MB 常驻，与"<40MB"指标自相矛盾。

**二进制拆分（第三轮评审 T1-3 的结论）**

| 产物 | Windows 子系统 | 用途 |
|---|---|---|
| `epo.exe` | console | CLI，保留控制台输出与退出码 |
| `epo-daemon.exe` | windowsgui | 常驻 + 托盘，开机自启**不闪黑框** |
| `epo-gui.exe` | windowsgui | Wails 界面，按需启动 |

单个 exe 同时当"控制台 CLI"和"无窗 daemon"必须靠 `FreeConsole()` 规避子系统冲突，结果是开机自启闪窗、或 `epo login` 没有任何输出。**拆成两个文件更干净**，代价只是多一个产物。

**托盘实现（已知难点，M1 前必须先验证）**

- 优先纯 Go 库（`energye/systray`）：**动手前确认它是否真的免 cgo**；若需要 cgo，CI 得装 mingw，与 §9 的构建脚本不符
- 兜底方案：自建 `Shell_NotifyIcon` + 隐藏窗口 + `GetMessage` 消息泵 + `runtime.LockOSThread`，这是"写对了才有图标"的低容错代码
- 若两条路都受阻：**降级为无托盘**，只驻留 daemon，认证功能完全不受影响

### 3.2 仓库结构

```
epo/
├─ cmd/
│  ├─ epo/                     # CLI（console 子系统）
│  ├─ epo-daemon/              # 常驻 daemon + 托盘（windowsgui 子系统）
│  └─ epo-gui/                 # Wails 前端
│     ├─ main.go / app.go
│     └─ frontend/             # Vue3 + Vite + TS
├─ internal/
│  ├─ portal/portal.go         # 协议无关接口
│  ├─ portal/drcom/            # Dr.COM 实现
│  ├─ probe/                   # 主备双探针 + 判定
│  ├─ daemon/                  # 状态机 / 退避 / 单实例 / IPC
│  ├─ creds/                   # DPAPI
│  ├─ netinfo/                 # WlanAPI：SSID、MAC、随机化状态
│  ├─ autostart/               # 注册表 Run
│  ├─ tray/  ├─ config/  └─ logx/
├─ profiles/jxau.json
├─ docs/{TECH_DESIGN.md,PROTOCOL.md,ADR/}
└─ .github/workflows/release.yml
```

---

## 4. 核心接口（协议无关）

```go
type Endpoint struct {
    Host     string            // 门户主机
    Port     int
    BasePath string            // "/eportal/"
    Extra    map[string]string // 跳转 URL 携带的网关参数（wlanacip / usermac 等）
}

type Portal interface {
    Probe(ctx context.Context, ep Endpoint, probeURL string) (State, Evidence, error)
    Login(ctx context.Context, ep Endpoint, c Credential) (Result, error)
    Logout(ctx context.Context, ep Endpoint) error
}
```

去掉了 `UserIP / AcIP / MAC` 等 drcom 专有字段，网关参数统一进 `Extra`。将来接深澜可实现同一接口，但 `BasePath` 仍是门户形态，因此只能承诺**降低改写成本**，不承诺"上层完全不变"。

---

## 5. 模块

### 5.1 凭据

- 存储：DPAPI（CurrentUser），路径 `%LOCALAPPDATA%\epo\secret.bin`
- **不用 `%APPDATA%`**：漫游目录会导致 DPAPI blob 跨机器解密失败
- 密码不走命令行 / 环境变量；CLI 输入无回显
- README 如实描述保护边界：**DPAPI 只挡跨用户与跨机器，挡不住同用户下的其他进程**

### 5.2 netinfo（读网络信息）

用 **WlanAPI**（`WlanQueryInterface`）而非解析 `netsh` 输出（后者随系统语言变化）：

- 当前 SSID / BSSID
- `dot11CurrentAddress` → 判断是否本地管理位（随机 MAC）
- 关联状态

### 5.3 daemon

```
IDLE → PROBING → ONLINE
              ↘ PORTAL_UNKNOWN（拿不到网关参数，Code 5）
              ↘ LOGGING_IN → ONLINE
                          ↘ AUTH_FAILED（停止重试，等待人工复位）
     ONLINE → BACKOFF（探活连续失败）→ PROBING
```

- 周期探活：在线期 **5s**；掉线后立即登录，失败 3s 快速重试，连续失败 3 次后 45→60s 封顶；Wi-Fi 事件触发时立即探活
- **连续 2 次同结论才切换状态**，抑制抖动
- **全局频率上限**：10 分钟内最多 3 次登录
- **短存活检测**：登录成功但存活 < 60s，判为异常，进入长退避（60s）而非重置
- **AUTH_FAILED 复位入口**：修改凭据（`SaveConfig`）、手动「立即登录」、重新加载 profile，三者任一即复位
- **"已在线"兜底**：`result != 1` 时**不直接判失败**，先用**探针**复核（`chkstatus` 已证实不可用，不得使用）；若判在线则归 `ONLINE` 并记日志。否则会出现"能上网的用户被判成故障且不再自愈"
- **登录后复核**：`LOGGING_IN → ONLINE` 前必须有独立确认，防假成功
- **新增 `PORTAL_UNKNOWN` 状态**，对应 Code 5，仅在前置阶段（拿不到网关参数）出现
- **事件触发优先**：`WlanRegisterNotification` 监听连接/断开，事件到达立即探活
  - **两个指标分开**：Wi-Fi 断连→恢复 P95 < 15s；网关踢线/会话过期**不产生 WLAN 事件**，只能靠周期探活，发现 P95 ≤ 2×周期（≤240s）
  - 「连续 2 次同结论」只用于**周期探活**；事件触发路径可立即切换状态
- **托盘运行在 daemon 进程内**：状态 / 立即登录 / 暂停自动登录 / 完全退出 / 打开主界面（最后一项通过管道通知 daemon 拉起 GUI）
- 单实例 + 管道 IPC（JSON Lines）

### 5.4 GUI（Wails v2）

- 绑定：`Status / Login / Logout / GetConfig / SaveConfig / Doctor / ExportDiagnostics`
- 状态推送：`runtime.EventsEmit`，前端订阅
- 三页：首次向导、主界面、设置
- **无托盘、无常驻**：关闭窗口 = 进程退出，WebView2 随之释放
- 启动方式二者等价：托盘菜单「打开主界面」/ 用户双击 exe
- 托盘点击只负责"拉起 GUI"，不承载任何认证状态

---

## 6. 关键设计

### 6.1 探活与判定（v0.2 的 P0 缺陷，已重做）

**背景实测**：`http://captive.apple.com/generate_204` 返回 **200 + Tengine 伪 "Success" 页**（正常应为 204 空响应）；`ncsi.txt` 返回 `Microsoft NCSI`；门户 `visit_blacklist` 恰含这些地址。说明系统级探测地址被网关特殊处理，**用作探针必然误判在线**。

**禁止项**：任何出现在 `visit_blacklist` 中的地址（`generate_204`、`captive.apple.com`、`ncsi.txt`、`www.msftncsi.com`、`success.txt` 等）。

**主备双探针**：

| | 地址 | 期望串 `expect` | 作用 |
|---|---|---|---|
| 主 | `http://www.jxau.edu.cn/?epo=<rand>` | `jxau.edu.cn` | 实测：离线 302 到门户带全参；在线 200/206 且正文命中 expect |
| 备 | `http://www.baidu.com/` | `baidu.com` | 主探针异常时交叉验证 |

> `expect` 必须是正常状态下正文前 8 KB 内必然出现的片段，**优先使用 ASCII**——学校页面多为 GBK 编码，中文串按 UTF-8 比对必然失败。

两者结论不一致 → 按**未认证**处理并记日志（宁可重试，不可漏登）。

> 措辞说明：未认证时两个探针会被同一个网关重定向到同一个 `a79.htm`，所以"交叉验证"实际只能覆盖**单个站点 5xx/超时**，不能算独立证据源。

**判定信号（三项合判，禁止只看状态码）**：

| 结论 | 条件 |
|---|---|
| **在线** | 3xx 且 `Location` 主机 **与探针同站**（精确主机相等，或 eTLD+1 命中 profile 的 `probe_hosts`，如 `jxau.edu.cn`） |
| **未认证** | 3xx 且 Location 主机 **不同站**（内网 IP、`a79.htm` / `eportal`、或任何外来主机） |
| **在线** | **2xx（含 206）** 且响应体**命中**探针的 `expect` 串 |
| **未认证** | 2xx 但**未命中** `expect`（含空正文、透明劫持页） |
| **未认证** | 其余所有未覆盖组合（未知分支保守处理） |
| — | 5xx / 超时 / DNS 失败：**不计为证据**；两个探针都失败 → **Code 7（探针不可用）**，不得归为"网络不可达" |

> **为什么必须包含 206**：v0.6 为省流量加了 `Range: bytes=0-1023`，实测**在线时学校主页返回 206**（1024 字节）。若判定表只认 200/204，会在已联网状态下判成"未认证"，每 10 分钟无效重试。**判定口径一律用 2xx，不要假设一定是 200。**
>
> **为什么按"同站"而非精确主机**：真实站点常见 `www.x.edu.cn → x.edu.cn` 与反向跳转，精确比较会把在线判成未认证。上限：仅对探针自身域名放宽，任何落到门户/内网主机的结果仍判未认证。

> **判定口径只有一句：请求是否到达了探针自己的服务器**，不再枚举内网网段。
>
> v0.4 曾把「3xx 指向公网 / 301 到 HTTPS」判为未认证，**方向是反的**：已认证时学校站点自身就会 301 跳 HTTPS，那恰恰是在线证据。该错误由第二轮评审指出，此处已修正。

**判定方向（v0.9 修正）**：优先用**正向期望**——2xx 必须命中 `expect` 才算在线。原因是校园网关常见**透明劫持**：未认证时仍返回 200，只是正文被换成登录页。只做"排除登录页特征"会漏判任何未收录的新模板。

**登录页特征串仅是兜底**（仅当 `expect` 未配置时使用）：`WISPAccessGatewayParam`、`MessageType>110`、`DDDDD`、`upass`、`Dr.COMWebLogin`、`ACSetting`、`<input type="password"`。

**不做裸词匹配**：`eportal`、`user_password` 这类通用词可能出现在正常页面的脚本或文本里，一旦裸匹配，**在线会被误判成未认证**（与 206 那个坑同类）。

**"疑似拦截页"的判据**（满足任一才继续读正文，否则读完 1 KB 即止）：

1. `Content-Length` 已知且 **< 16 KB**
2. 响应头 `Server` 含 `NetEngine` 或 `Drcom`
3. `Content-Length` 未知（chunked）——按疑似处理，读满 1 KB 再看特征

> **M0 实测结论（2026-11-02）**：未认证时，学校主页 / 百度 / `generate_204` 三个地址**全部返回 302**，`Location` 统一为 `http://172.16.121.9/a79.htm?userip=…&wlanacip=172.16.x.x&usermac=…`，响应体是标准 WISPA 拦截 XML（`Server: NetEngine Server 1.0`）。因此判定规则可定稿为：**`Location` 主机 ≠ 探针域名 → 未认证**。
>
> 补充：`generate_204` 在**在线**时返回 200（`Server: nginx`，68 字节，不同 CDN 边缘返回 Tengine/nginx 的 "Success" 页），在**离线**时正常 302。它其实可用，但为防止学校后续把它加入放行名单，仍不作为默认探针。

**在线态实测（2026-11-02，登录状态下）**

| 探针 | 无 Range | 带 Range |
|---|---|---|
| `www.jxau.edu.cn` | 200，91 KB，无跳转 | **206**，1024 字节 |
| `www.baidu.com` | 302 → `https://www.baidu.com/…`（同站） | 302（同左） |
| `generate_204` | 200，nginx，68 字节 | 200（同左） |

补充约束：

- 必须走 **HTTP**，禁用 `AllowAutoRedirect`，只读 `Location`
- 随机 query 仅对防 CDN 缓存 200 有效，**对 302 无用**（不作为通用手段）
- 5xx / 超时**不计为证据**（主站维护期不应被误判为掉线）
- 主探针离线行为已实测（302 → 门户带全参），profile 中已落值，不再是阻塞项

**流量控制（本轮新增，v0.6 的隐藏成本）**

| 规则 | 内容 |
|---|---|
| 只读响应头 | 请求带 `Range: bytes=0-1023`；服务端忽略 Range 时，**读满 1 KB 立即关闭连接**。判定所需的状态码与 `Location` 全在头部 |
| 正文按需 | 仅当「200 且疑似拦截页」才继续读，**上限 8 KB** 用于特征匹配；其余情况一律不读 |
| 单次预算 | **≤ 10 KB**（头部 + 最多 8 KB 正文） |
| 周期分级 | 在线期 **5s**（掉线发现延迟即恢复延迟）；掉线后**立即登录**，失败 3s 快速重试；连续失败 3 次后 45→60s 封顶 |
| 全天总量 | **≈ 7 MB**（同类项目实测 0.4 KB/次 × 5s；对比：完整下载学校主页约 800 MB/天） |

> 原因：学校主页 HTML 通常 100–300 KB，若每 30 秒完整拉取一次，仅探活一项每天就产生近 1 GB 流量，在计费校园网环境下不可接受，也会在网关日志留下异常特征。

**辅助信号**：网关自带 `GET /drcom/chkstatus?callback=dr1002&jsVersion=4.X`。实测**在线与离线都返回 `result:0`**，无法区分状态，**只作诊断信息记录，不参与判定**。

### 6.2 凭据发送前四项校验（阻断项不满足即拒绝登录）

> 背景：开放 Wi-Fi + 明文 HTTP，同名热点可钓鱼；链路上同 AP 用户可嗅探。工具无法消除后者，但必须拒绝前者。

| # | 校验 | 性质 | 不通过时 |
|---|---|---|---|
| 1 | 当前 SSID == profile `ssid` | **阻断** | 拒绝登录，提示"当前网络不是校园网" |
| 2 | 门户主机 **必须 ∈ 静态白名单**（交集，**不是并集**） | **阻断** | 拒绝登录，提示"门户地址异常"并记诊断 |
| 3 | 网卡未开随机 MAC（`dot11CurrentAddress` 本地管理位） | **仅告警** | 提示并放行（Win11 默认可能开启，硬阻断会让工具在默认配置下不可用） |
| 4 | **默认网关 MAC 与首次信任值一致**（TOFU） | **阻断** | 拒绝登录，提示"网络环境异常，可能连接到仿冒热点" |

**网关 MAC 首次信任（TOFU）细则**

- **记录条件（防投毒）**：必须同时满足「登录返回 `result:1`」**且**「探针复核判定在线」才记录网关 MAC。只看 `result:1` 会被假门户喂一个 `{"result":1}` 就把攻击者的 MAC 写进信任列表
- **读取方式**：Go 内用 `GetIpNetTable2` / `SendARP` 取网关链路层地址（`Get-NetNeighbor` 是 PowerShell cmdlet，Go 调不到）；取之前先触发一次流量，避免 ARP 条目处于 Incomplete
- **校验时机**：每次发起登录前比对，不一致即阻断
- **阻断不死锁**：阻断弹窗/界面内必须直接提供「**信任当前网络并继续（本次）**」按钮。只写"去设置页改"等于把换楼栋的用户永久卡死
- **信任变更**：命中列表任意一条即通过；列表按 **LRU 保留最近成功登录的网关**（上限 8 条），而不是 FIFO——FIFO 会把长期有效的网关挤掉
- **能抬高的成本**：建同名 SSID 时，攻击者的"网关"MAC 与学校真实网关不同 → 被识别
- **不能宣称的**：MAC 可被修改、同二层可做 ARP 欺骗，**这不是密码学验证**，只能抬高门槛（ADR-009 的措辞已同步下调）
- **防不住**：跨网段看不到门户 `172.16.121.9` 的 MAC；链路层嗅探明文凭据同样无解
- 该值 `null` / 空列表表示"尚未建立信任"，此时**不阻断只记录**（首次使用不该被卡住），并在向导中提示「请在校园网内完成首次配置」

三条硬约束：

- **`usermac` 只做原样回传，不做任何比对** —— 它是不透明密文，与裸 MAC 无法相等；v0.4 写的"MAC 与 `usermac` 一致"在实现层根本写不出来，硬写的结果只会被悄悄跳过，让校验退化为形同虚设
- **重定向解析出的主机只能用于诊断与一致性提示，不得作为放行依据** —— 它本身由不可信网络提供；v0.4 的并集写法等于让攻击者自己决定白名单
- **必须如实承认**：开放 WiFi + 明文 HTTP 下，网关身份无法被密码学验证，这些校验只能抬高门槛，**不能宣称"已防住钓鱼"**

**注意（评审建议修正）**：不能校验"门户主机与网关 IP 一致"——实测本机网关 `10.100.0.1`、门户 `172.16.121.9`，**不在同一网段**，该规则会让江农 100% 拒绝登录。正确语义是"来源一致性"。

README 必须写明：凭据在校园网内以**明文 HTTP** 传输，这是学校网关的限制，非本工具可选项。

### 6.3 协议实现（v0.4：按真实抓包重写）

原 v0.3 依据门户页面配置推测为 `POST /eportal/?c=ACSetting&a=Login` + `DDDDD/upass`。**实测并未调用该接口**，真实流程如下。

**前置步骤（登录链路必须有，v0.4 遗漏）**

1. `GET` 探针地址（禁止跟随重定向），从 `Location` 的 query 中取出 `wlanacip`、`usermac`、`userip`
2. 三项取不到 → 返回 **Code 5（门户不可识别）**，**不发起登录**
3. 用取到的值构造登录请求

> ❗待验证（M0 新增项）：被踢下线 / 会话超时后，网关是否仍返回**带参数**的 302。若此时返回"您已在线"页且不带参数，登录链路会死锁——比判定算法更致命。

**登录请求：GET + JSONP**

```
GET http://172.16.121.9:801/eportal/portal/login
    ?callback=dr1003
    &login_method=1
    &user_account=%2C0%2C<学号>%40cmcc
    &user_password=<明文密码>
    &wlan_user_ip=<本机 IP>
    &wlan_user_ipv6=
    &wlan_user_mac=<跳转 URL 的 usermac>
    &wlan_ac_ip=<跳转 URL 的 wlanacip>
    &wlan_ac_name=
    &jsVersion=4.2.1
    &terminal_type=1
    &lang=zh-cn
    &v=<随机数>
```

| 参数 | 来源 | 说明 |
|---|---|---|
| `user_account` | `,0,` + 学号 + `@` + 运营商 | 移动 `@cmcc`；电信/联通后缀待收集 |
| `user_password` | 凭据 | 明文，需 URL 编码 |
| `wlan_user_ip` | **优先 302 的 `Location.userip`**；缺失时回退 WLAN 接口 IPv4 | 网关视角的地址才可信；多网卡/NAT 下本机 IP 可能与网关认知不一致，两者同时记日志 |
| `wlan_user_mac` | 跳转 URL 的 `usermac` | **不透明长串，原样回传，禁止自行拼 MAC** |
| `wlan_ac_ip` | 跳转 URL 的 `wlanacip` | 每次会话可能不同（实测见过 `.14` 与 `.148`） |
| `jsVersion` | 固定 `4.2.1` | — |
| `terminal_type` | 固定 `1` | PC 终端 |
| `v` | 随机数 | 防缓存 |

**响应：JSONP**

```
dr1003({"result":1,"msg":"Portal协议认证成功！"});
```

判定：`result == 1` 为成功；其余为失败，失败文案直接取 `msg` 原样展示。

**请求头**：需带 `Referer: http://172.16.121.9/`（抓包实测存在，建议保留）。

**编码**：本接口返回 UTF-8，无需 GB2312 解码。GB2312 只在直接读取 `a79.htm` 页面时使用，而该页面在新流程中仅作诊断，不参与登录。

**兼容分支：删除**。M0 实测 `/eportal/?c=ACSetting&a=Login` 返回的是 EPortal 的 Vue 页面（HTML，`<title>EPortal</title>`），并非可提交的登录表单 —— 该旧接口在本校已废弃。不实现回退逻辑，避免"一次失败触发双请求"。

**超时**：连接 5s / 整体 10s。

**登出接口：未验证（本轮审查新发现）**。HAR 与 M0 都只抓了登录，没有捕获注销请求；而接口与 GUI 都预留了 `Logout`。M1 前补抓一次注销，**拿不到就在 GUI 隐藏该入口**，不要实现一个没验证过的功能。

### 6.4 WebView2

本机 Win11 Build 26200，WebView2 **139.0.3405.86** 已装。启动时检测注册表 `pv`，缺失则弹原生提示 + Evergreen 下载链接 + **建议改用 CLI**（CLI 功能等价）。

---

## 7. 配置

```json
{
  "schema_version": 1,
  "id": "jxau-stu",
  "display_name": "江西农业大学学生网",
  "portal": { "type": "drcom-eportal", "host": "172.16.121.9", "port": 801 },
  "network": {
    "ssid": "JXAU-Stu",
    "probes": [
      { "url": "http://www.jxau.edu.cn/?epo=1", "expect": "jxau.edu.cn" },
      { "url": "http://www.baidu.com/?epo=1", "expect": "baidu.com" }
    ],
    "probe_hosts": ["www.jxau.edu.cn", "jxau.edu.cn"],
    "gateway_mac": []
  },
  "login": {
    "carrier": "cmcc",
    "carrier_suffixes": { "cmcc": "cmcc", "ctcc": "telecom", "cucc": "unicom", "campus": "" },
    "extra_params": {
      "jsVersion": "4.2.1",
      "terminal_type": "1",
      "login_method": "1",
      "lang": "zh-cn"
    }
  },
  "policy": {
    "online_interval_sec": 5,
    "offline_interval_sec": 3,
    "backoff_max_sec": 60,
    "max_logins_per_10min": 3,
    "short_session_sec": 60
  }
}
```

说明：

- `probes` 每项含 `expect`（正常状态正文必现片段，优先 ASCII）；`probe_hosts` 用于 3xx 的"同站"比较
- 周期语义：`online_interval_sec` = 在线时探测间隔（5s，直接决定掉线恢复速度）；`offline_interval_sec` = 失败后的快速重试间隔（3s）；两者都不替代事件触发
- `login.carrier_suffixes`：移动 `cmcc` 为本地实测；电信 `telecom` / 联通 `unicom` 取自同类 ePortal 项目实测，**尚未在本校验证**，可随时覆盖
- `jsVersion` / `terminal_type` / `login_method` / `lang` 全部进 `extra_params`，网关升级时可改配置而**无需发版**
- `carrier` 支持 `cmcc` / `ctcc` / `cucc` / `campus`；未采集到的后缀允许用户手工填写，失败文案提示"若为电信/联通用户，请确认运营商选项"
- `wlan_user_ip` **优先取 302 的 `Location.userip`**，缺失才回退 WLAN 接口 IPv4（多网卡场景）
- `gateway_mac` 为**列表**（LRU，上限 8）；仅在「`result:1` 且探针复核在线」后写入；空列表只记录不阻断；阻断提示须带「信任当前网络并继续」

用户目录 `%LOCALAPPDATA%\epo\`：`config.json`、`secret.bin`、`logs\`、`diagnostics\`。

---

## 8. 安全

| 面 | 措施 |
|---|---|
| 凭据 | DPAPI 用户域；不进命令行/环境变量；日志脱敏；README 如实说明保护边界 |
| 传输 | 明文 HTTP 为学校限制，文档如实告知；不做"已加密"表述 |
| 钓鱼防护 | §6.2 四项校验：SSID、门户主机白名单（交集）、随机 MAC 告警、**网关 MAC 首次信任** |
| 不可消除风险 | 开放 WiFi + 明文 HTTP + GET 传参 → 同 AP 可嗅探、URL 会进访问日志；README 必须原话说明 |
| 配置 | 门户主机白名单校验 |
| 权限 | 普通用户，不注册服务、不提权 |
| 供应链 | 仅官方 Go 库 + Wails |
| 遥测 | 无 |
| 发布 | SHA256 清单；建议核对 |

---

## 9. 构建与发布

```bash
npm ci --prefix cmd/epo-gui/frontend
go test ./...
go build -ldflags "-s -w" -o epo.exe ./cmd/epo
go build -ldflags "-s -w" -H windowsgui -o epo-daemon.exe ./cmd/epo-daemon
go install github.com/wailsapp/wails/v2/cmd/wails@v2.9.2
wails build -platform windows/amd64   # 在 cmd/epo-gui 内执行
```

CI（windows-latest）：

```yaml
on: { push: { tags: ["v*"] } }
permissions:
  contents: write          # 新仓库 GITHUB_TOKEN 默认只读，缺这行发布必然失败
jobs:
  build:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.21" }
      - uses: actions/setup-node@v4
        with: { node-version: "20" }
      - run: npm ci
        working-directory: cmd/epo-gui/frontend
      - run: go test ./...
      - run: go build -ldflags "-s -w" -o epo.exe ./cmd/epo
      - run: go build -ldflags "-s -w" -H windowsgui -o epo-daemon.exe ./cmd/epo-daemon
      # wails CLI 必须锁版本，@latest 不可复现
      - run: go install github.com/wailsapp/wails/v2/cmd/wails@v2.9.2
      # wails 必须在 cmd/epo-gui 内执行，否则找不到 wails.json
      - run: wails build -platform windows/amd64
        working-directory: cmd/epo-gui
      - name: Checksums
        run: |
          Get-FileHash epo.exe -Algorithm SHA256 | Out-File checksums.txt
          Get-FileHash epo-daemon.exe -Algorithm SHA256 | Out-File checksums.txt -Append
          Get-FileHash cmd/epo-gui/build/bin/epo-gui.exe -Algorithm SHA256 | Out-File checksums.txt -Append
      - name: Publish
        uses: softprops/action-gh-release@v2
        with:
          files: |
            epo.exe
            epo-daemon.exe
            cmd/epo-gui/build/bin/epo-gui.exe
            checksums.txt
```

分发：GitHub Release 主源，**Gitee 同步镜像**（目标人群网络可达性）。无自动更新，README 说明升级方式。无代码签名 → SmartScreen 会提示"未知发布者"，属于已知且不可消除的限制。

---

## 10. 降级路径

| 失败点 | 策略 |
|---|---|
| WebView2 缺失 | 提示改用 CLI，功能等价 |
| GUI 构建失败 | 只发 CLI，Release 照常 |
| 托盘不可用 | 用 `epo.exe gui` 直接启动界面；认证不受影响 |
| 探针误判 | `doctor` 支持手动指定探针地址 |

---

## 11. 错误码

| Code | 含义 | 文案 | 重试 |
|---|---|---|---|
| 0 | 成功 | — | — |
| 1 | 网络不可达 | 当前未连接到网络，请检查 Wi-Fi | 是 |
| 2 | **认证被拒**（附原始响应摘要） | 认证失败：<服务端提示> | 否 |
| 5 | 门户不可识别 | 未识别到校园认证门户，请运行诊断 | 有限 |
| 6 | 配置无效 | 配置文件格式错误 | 否 |
| 7 | **探针不可用** | 校园网探针无响应，请稍后重试 | 是（退避） |

> 原 v0.2 的 "密码错误 / 账号锁定 / 单终端限制" 三档已合并。现已取得**成功响应**（`{"result":1,...}`）与**失败响应**（`{"result":0,"msg":"账号不存在","ret_code":1}`），但失败样本只覆盖"账号不存在"一种。**其余原因在拿到样本前，Code 2 一律透传 `msg`，不做归类。**

补充：

- **Code 5 的唯一触发点**是前置阶段拿不到 `wlanacip` / `usermac` / `userip`（见 §6.3），不对应任何"响应异常"
- Code 3 / 4 为**预留未启用**，实现时不得映射，避免无证据归类
- Code 2 触发后必须先做一次在线复核，确认是"真失败"再进入 `AUTH_FAILED`（见 §5.3）
- M0 已取得一个失败样本：`dr1003({"result":0,"msg":"账号不存在","ret_code":1});`。`msg` 是可直接展示的中文文案，`ret_code` 可供电程序分支；但**密码错误 / 账号锁定 / 单终端限制的样本仍缺**，暂不据此细分

---

## 12. 可观测性

- `epo doctor`：网卡、SSID、MAC 随机化状态、IP/网关、双探针原始响应、Endpoint、登录请求（脱敏）、响应摘要；额外检查自启路径有效性
- 日志：`log/slog` JSON Lines，5 MB × 3 轮转，GUI 内可查看导出
- 诊断包：`%LOCALAPPDATA%\epo\diagnostics\*.zip`
  - **逐项脱敏**：`user_password`、`wlan_user_mac`（`usermac`）、学号、SSID/BSSID 全部单独替换，不要只对整条 URL 打码（容易漏字段）
  - 生成后提示"贴到 Issue 前请自行检查一遍"

---

## 13. 非功能指标

| 指标 | 目标 |
|---|---|
| CLI 冷启动 | < 200 ms |
| 认证耗时（发起→成功） | < 3 s |
| daemon 常驻内存（**含托盘图标**） | **< 40 MB** |
| GUI 内存 | 仅在打开主界面时存在；关闭窗口即释放，**不参与空闲态统计** |
| GUI 冷启动（首次，需建 WebView） | < 8 s |
| GUI 冷启动（非首次） | < 3 s |
| 空闲 CPU | ≈ 0（在线期 5s 一次探针，单次约 2 ms） |

**体积与流量预算**

| 指标 | 目标 | 说明 |
|---|---|---|
| `epo.exe`（CLI） | 6–9 MB | `-ldflags "-s -w"` |
| `epo-daemon.exe`（daemon + 托盘） | 8–12 MB | 含托盘图标与 Win32 资源 |
| `epo-gui.exe`（Wails） | 15–22 MB | 前端 dist 直接内嵌，体积算进 exe |
| Release zip | 14–20 MB | 三个产物压缩后 |
| 单次探针流量 | ≤ 10 KB | 头部 + 最多 8 KB 正文（口径与 §6.1 统一） |
| 全天探针流量 | ≈ 7 MB | 5s 周期、命中即停，同类项目实测 0.4 KB/次 |
| 日志磁盘上限 | 15 MB | 5 MB × 3 轮转 |

---

## 14. 测试

| 层级 | 内容 |
|---|---|
| 单元 | URL 参数构造、**JSONP 解析**、**判定算法全分支（2xx/206、同站 3xx、拦截页、未知响应）**、退避与频率上限 |
| Golden | 固化探针响应样本与登录响应样本 |
| 集成 | 真实网络，环境变量开关，CI 跳过 |
| 手工 | Win11（本机）+ 一台无 WebView2 的 Win10 |

---

## 15. 风险

| 风险 | 缓解 |
|---|---|
| 网关升级参数失效 | `doctor` 定位；参数走 profile 可热改 |
| 杀软误报 | 源码自建指引 + SHA256 |
| 账号风控 | 频率上限 + 短存活检测 + 失败即停 |
| WebView2 缺失 | 检测 + CLI 降级 |
| 学校策略变动（验证码/开无感知） | README 声明"非官方工具，可能随时失效" |

---

## 16. 里程碑

| 阶段 | 交付 | DoD |
|---|---|---|
| M0 | 抓包与探针实测（用 curl / 浏览器，**不依赖 doctor**） | 见 §17 清单全部有结论 |
| M1 | 协议包 + 探针判定 + CLI `login/status/logout/doctor` | 全链路跑通；判定算法单测覆盖所有分支 |
| M2 | daemon 常驻 + 退避 + 频率上限 + IPC + 自启 | 断网自动恢复；重启自启；GUI 未启动时 CLI 可独立工作 |
| M3 | Wails GUI（**托盘已归属 daemon**） | 向导三步连通；关闭窗口后 daemon 与托盘仍在跑 |

---

## 17. M0 实测清单

| # | 项 | 目的 | 状态 |
|---|---|---|---|
| 1 | 未认证时 `GET http://www.jxau.edu.cn/?epo=<rand>` 的真实响应 | 决定判定算法形态 | ✅ **302 → 门户，带全部参数** |
| 2 | 同上记录 `http://www.baidu.com` | 定主备探针 | ✅ 与主探针返回一致 |
| 3 | 抓包确认请求模板与参数来源 | 定请求模板 | ✅ 已由 HAR 完成（§6.3） |
| 4 | 同账号二次登录响应 | 是否存在单终端限制 | ❗未完成 |
| 5 | 失败响应样本 | 错误码细分证据 | ⚠️ 已得"账号不存在"样本，其余原因缺 |
| 6 | `usermac` 与真实 MAC 的关系 | 定校验规则 | ✅ 已确认非裸 MAC；随机化影响未测 |
| 7 | 注销后立即探活 vs 从未登录探活 | 登出态一致性 | ✅ 注销后仍返回带参 302（本次即该状态） |
| 8 | 被踢下线 / **会话超时**后是否仍返回带参 302 | 前置步骤是否死锁 | ⚠️ 注销态已验证；**会话超时未测，M1 前必测** |
| 9 | **登录前参数解析** | 从 302 的 Location 提取 `wlanacip`/`usermac`/`userip` | ✅ 已提取到 `172.16.x.x` / `opaque…` / `10.100.7.249` |
| 10 | **在线态三探针响应** | 补齐判定表的"在线"分支 | ✅ 2026-11-02 实测：jxau 200/**206**、baidu 302 同站、generate_204 200 |
| 11 | **登出请求** | 接口与 GUI 已预留 `Logout`，但从未验证 | ❗未完成（M1 前必测，否则隐藏入口） |

---

## 18. 架构决策记录

| 编号 | 决策 | 理由（v0.3 重写） |
|---|---|---|
| ADR-001 | Go 而非 C#/.NET | 诚实记录：Windows-only 场景下 C# 在托盘、DPAPI、单文件发布上确实更省心，且无 WebView2 依赖。选择 Go 的实际理由是**作者熟悉度 + 保留未来跨平台余量 + 单文件交叉编译成本低**。放弃 C# 的代价是可接受的，但若后续 Go 生态在托盘/系统集成上反复受限，**可复议** |
| ADR-002 | Wails v2 而非 Fyne/Walk | GUI 仅三页，Wails 的界面质量对传播有帮助；WebView2 是最大兼容性负债，因此**把 CLI 设为默认分发物、GUI 为增强**，并在启动时检测降级。v2 稳定、v3 alpha，暂不迁移 |
| ADR-003 | 单校专用而非通用框架 | 单人项目，范围是最大风险；保留协议无关接口与 profile，使扩展不必重写 |
| ADR-004 | 探针用学校主页 + 百度双探针 | 实测：离线时两者均 302 到门户；在线时学校主页 200/206、百度 302 同站。双探针只能覆盖单站 5xx/超时，不是独立证据源 |
| ADR-005 | daemon 独立进程而非内嵌 GUI | 内嵌会导致 GUI 崩溃即断网，且为 30 秒一次的请求常驻 250MB 不划算 |
| ADR-006 | 主协议走 `/eportal/portal/login`（JSONP GET）而非旧版表单 | 抓包证实实际流程；M0 实测旧版 `ACSetting/Login` 返回 Vue 页面而非登录表单，**已删除该分支** |
| ADR-007 | `chkstatus` 仅作旁路记录，不参与在线判定 | 实测在线与离线均 `result:0`，无法区分状态；保留为诊断信息 |
| ADR-008 | 托盘运行在 daemon 而非 GUI | 托盘若依附 GUI，则 WebView2 必须常驻（约 250MB），与"空闲态 <40MB"的目标直接冲突。放 daemon 后，重界面按需加载、用完释放 |
| ADR-009 | 采用默认网关 MAC 首次信任，而非 BSSID 白名单 | 校园网 BSSID 随 AP 漫游频繁变化，固定集合会导致频繁误报；网关 MAC 在同一网络内相对稳定。**注意：MAC 可伪造、ARP 可欺骗，这只是抬高成本，不是密码学验证** |

---

## 19. 适用范围与限制

- 命名互斥体与管道为**单用户单会话**设计，不支持 RDP 多用户并发
- 自启写入的是 `epo-daemon.exe` 的**绝对路径**，用户移动安装目录后失效；`doctor` 会检查此项
- `HKCU\Run` 在用户登录桌面后才生效，实际效果是"登录后约 30 秒联网"，向导与 README 需如实说明
- 不支持网关开启验证码、无感知认证、MAC 白名单等策略变更后的场景

---

## 20. 第二轮评审（v0.4 复审）整改对照

| 编号 | 问题 | 处置 |
|---|---|---|
| R0-1 | 3xx 指向公网/HTTPS 被误判为未认证（**方向性错误**） | §6.1 判定口径改为「Location 主机是否等于探针自身域名」 |
| R0-2 | 校验 #2 用并集，重定向主机由攻击者可控 | §6.2 改为**必须命中静态白名单**；重定向主机仅作诊断 |
| R0-3 | 校验 #3 要求 MAC 与 `usermac` 相等，实现层写不出来 | §6.2 拆分：随机 MAC **仅告警**；`usermac` **只回传不比对** |
| R1-1 | 登录前置步骤（取 `wlanacip`/`usermac`）缺失 | §6.3 补前置三步 + Code 5 触发点；M0 新增第 8 项 |
| R1-2 | `epo login` 无 daemon 时行为未定义 | §3.1 规则 5 明确：拉起 → 等管道就绪 → 保留 daemon |
| R1-3 | 托盘"退出"与独立 daemon 语义冲突 | §5.4 拆为「暂停」与「完全退出」两项 |
| R1-4 | "已在线"返回 `result≠1` 会被锁进 AUTH_FAILED | §5.3 增加在线复核兜底；§11 同步 |
| R1-5 | 仅支持 `@cmcc` | §7 支持四种运营商 + 手工后缀；失败文案提示 |
| R1-6 | GUI 拉起的 daemon 未定义 detach 方式 | §3.1 规则 6/7：`DETACHED_PROCESS`、不继承句柄、抢锁失败静默退出 |
| R1-7 | CI 四处硬伤 | §9：锁 wails 版本、在 `cmd/epo-gui` 内构建、修正产物路径、补发布步骤 + `-ldflags` |
| R2-1 | `primary_probe` 写死与纪律冲突 | §7 改为 `null` |
| R2-2 | 协议参数硬编码 | §7 全部移入 `extra_params` |
| R2-3 | GET 传密码留痕面更大 | §6.2 / README 说明，不宣称"仅明文 HTTP" |
| R2-4~11、R3-1~3 | 事件触发、多网卡、脱敏、状态补齐、5xx 口径、接口措辞 | 分别落入 §5.3 / §7 / §12 / §11 / §6.1 / §4 |

进入 M1 前的三件事：① 断网实测 M0 #1/#2；② 补失败样本与二次登录样本；③ 以上 P0 整改落代码。

---

## 21. v0.6 自省整改

| 问题 | 性质 | 处置 |
|---|---|---|
| 托盘放在 GUI，导致 250MB 常驻 | **资源指标自相矛盾** | §3.1 托盘迁入 daemon；GUI 按需启动、关闭即退出 |
| 静态白名单挡不住"冒充门户 IP"的同名热点 | **反钓鱼有缺口** | §6.2 新增第 4 项：默认网关 MAC 首次信任 |
| 安全边界此前只写了措施，未写"防不住什么" | 表述不诚实 | §8 新增"不可消除风险"行；§6.2 补充 TOFU 的能防/不能防 |
| 指标把空闲态与界面态混在一起 | 口径错误 | §13 明确 GUI 不参与空闲态统计 |
| 探针每 30s 完整拉取学校主页 | **每天近 1 GB 流量**，计费网络不可接受 | §6.1 改为只读响应头（`Range` + 1 KB 上限），正文仅在疑似拦截页时按需读 |
| 体积从无估算 | 用户无法预判 | §13 新增体积与流量预算表 |

**v0.6 后仍未解决、且工具无法解决的**：开放 WiFi 链路层的明文嗅探、GET 传参导致的 URL 日志留痕、学校侧开启验证码或策略变更导致的失效。这三项只能写进 README，不能靠代码消除。

---

## 22. 第三轮评审（v0.7 复审）整改对照

| 编号 | 问题 | 处置 |
|---|---|---|
| T0-1 | 在线态探针样本缺失，"封版"下早了 | ✅ 2026-11-02 补测：jxau 200/206、baidu 302 同站、generate_204 200；样本已写入 §6.1 |
| T0-2 | `Range` 引入 206，判定表未覆盖 | ✅ 判定口径改为 **2xx（含 206）**；实测在线时学校主页正是 206 |
| T0-3 | `Location` 精确主机比较，www 与非 www 会误判 | ✅ 改为**同站比较**（`probe_hosts` 或 eTLD+1） |
| T1-1 | `wlan_user_ip` 来源自相矛盾 | ✅ 改为**优先 `Location.userip`**，回退 WLAN IPv4 |
| T1-2 | TOFU 四处缺陷（表述/投毒/死锁/FIFO） | ✅ 记录条件加"探针复核在线"；阻断提示带**一键信任**；改 **LRU**；措辞下调为"抬高成本" |
| T1-3 | 托盘迁入 daemon 的隐藏成本 | ✅ **二进制拆分**为 `epo.exe`(console) / `epo-daemon.exe`(windowsgui) / `epo-gui.exe`；托盘库 cgo 验证列为 M1 前置 |
| T1-4 | 两条指标自相矛盾 | ✅ 拆为"Wi-Fi 断连恢复 P95<15s"与"会话过期发现 ≤2×周期"；流量口径统一为 ≤10 KB |
| T1-5 | CI 缺 `permissions` | ✅ 补 `contents: write` |
| T2-1~6 | 表格不同步、测试项、降级措辞、M3 描述、状态图、会话超时标注 | ✅ 均已修正 |
| T2-7/8 | 特征串含裸词、"疑似拦截页"未定义 | ✅ 删裸词 `eportal`/`user_password`；给出三条判据 |
| T2-9 | 5xx 被归为"网络不可达" | ✅ 新增 **Code 7（探针不可用）** |
| T2-10/11 | 周期语义重叠、`primary_probe` 未落值 | ✅ 改为 `online/offline_interval_sec`；探针已落值 |
| T2-12 | 公开文档含真实网关 MAC / 内网 IP | ⚠️ 仓库内文档需脱敏；`docs/M0-REPORT.md` 发布前替换为占位值 |
| T2-13 | `Get-NetNeighbor` 在 Go 用不了 | ✅ 改用 `GetIpNetTable2` / `SendARP` |
| T2-14 | 随机 query 与"减少日志特征"冲突 | ✅ 取舍：**优先防缓存**，仅对 2xx 探针追加 |
| T3-1 | 双探针"交叉验证"措辞过强 | ✅ 已降级为"只覆盖单站 5xx/超时" |
| T3-2 | 登录后立即复核可能撞上会话未生效窗口 | ✅ 复核加 2 次短重试（500ms 间隔） |
| T3-3 | `epo login` 拉起常驻进程语义突变 | ✅ 首次提示"已启动后台守护"；提供 `--no-daemon` |

**仍未完成（M1 前必做）**：会话超时/踢线后是否仍返回带参 302（T2-6）；同账号二次登录样本；密码错误等失败文案。三者都是"抓一次包"级别。

## 23. v0.9 对齐成熟开源项目

参考项目：[qs0712/drcom-eportal-autologin](https://github.com/qs0712/drcom-eportal-autologin)（同协议、江西实测）、[HEX9CF/CupidConnector](https://github.com/HEX9CF/CupidConnector)（Go）、[BeiningWu/shu-net-keeper](https://github.com/BeiningWu/shu-net-keeper)（Rust 多平台）、[NatsumeHSY/CampusNetClient](https://github.com/NatsumeHSY/CampusNetClient)（GUI + 服务）、[mchome/dogcom](https://github.com/mchome/dogcom)（C）。

| 编号 | 参照发现 | 处置 | 落点 |
|---|---|---|---|
| C-1 | 网关**透明劫持**：未认证时也可能返回 200，只排除登录页特征会漏判 | 判定改为**正向期望**：2xx 必须命中 `expect` 才算在线；特征串降为兜底 | `probe/judge.go`、§6.1 |
| C-2 | 探测间隔直接决定掉线恢复体感，同类项目从 20s 压到 5s | 在线期 **5s**、失败 **3s** 快速重试、连续失败 3 次后 45→60s 封顶；流量仍约 7 MB/天 | `config`、§6.1、§13 |
| C-3 | 电信/联通后缀使用 `@telecom` / `@unicom`，与文档原写 `dx`/`lt` 不符且未实测 | 后缀表**配置化**（`login.carrier_suffixes`），默认改为 telecom/unicom，可随时覆盖 | `drcom/params.go`、§7 |
| C-4 | `wlan_user_ip` / `wlan_user_mac` 实测并非认证必填 | 三参数缺失**只告警不阻断**，仍尝试登录 | `cmd/epo` |
| C-5 | 重复登录返回"已经在线"属正常 | `result!=1` 但 msg 含"已经在线" → 判为成功 | `drcom/response.go` |
| C-6 | 自启更稳的做法是计划任务优先、启动文件夹兜底，配合 `wscript.exe` 无窗口 | M2 按此实现，弃用单一的 `HKCU\Run` | 待实现（§5.3） |
| C-7 | PowerShell 同类守护常驻 67–86 MB，凭据以明文存 `config.json` | 本项目 Go daemon 目标 < 40 MB、凭据 DPAPI 加密，属差异化优势，写入 README | README、§8 |

**测试**：整改后 5 个包共 46 个用例全部通过（`drcom` 12、`probe` 14、`guard` 8、`creds` 2、`netinfo` 4 + 解析用例），`go build ./...` 与 `go vet ./...` 均无输出。

## 24. M2 实现记录

| 项 | 实现 | 验证 |
|---|---|---|
| 守护状态机 | `internal/daemon`：5s 探测 / 3s 重试 / 3 次后 45s / 封顶 60s；10 分钟最多 3 次；短存活进长退避；登录后 2 次复核 | 6 个假时钟用例 |
| 单实例 | `internal/instance`：命名互斥体 `Local\epo-daemon` | 编译通过（需双进程手测） |
| 网关 MAC（TOFU） | `netinfo.arpMAC` 用 `SendARP` 读默认网关链路层地址；`internal/trust` 持久化 LRU 列表（上限 8） | 真机实测 `gwMAC=xx:xx:xx:xx:xx:xx`，与 M0 记录一致 |
| 启动记录时机 | `daemon.Deps.OnVerified`：仅在"登录成功且复核在线"后记录，防投毒 | 逻辑接点已接 |
| 开机自启 | `internal/autostart`：写 `HKCU\Run`，指向 windowsgui 的 `epo-daemon.exe`，无需管理员 | `epo autostart status` 冒烟通过 |
| 凭据 | `epo save-cred` 用 DPAPI 保存，`epo daemon` 启动时解密 | creds 往返测试通过 |

**途中修掉的一个真实缺陷**：`MIB_IPFORWARDROW` 的地址是网络序，原实现按大端取值，导致网关显示为 `1.0.100.10`（正确值 `10.100.0.1`）。已改为按小端还原并补充 `TestIPv4FromMIB` 回归用例；`SendARP` 的目标地址同步修正。

**已知未做**：GUI（M3）、IPC（M3）。

## 25. 真机联调记录（2026-11-07）

环境：江西农业大学 `JXAU-Stu`，Windows 11，真实注销触发掉线。

| 时间 | 事件 |
|---|---|
| 13:03:38 | 守护启动 |
| 13:03:42 | 判定在线 |
| 13:03:53 | **检测到掉线**（用户点击注销后） |
| 13:03:57 | 记录网关 MAC `xx:xx:xx:xx:xx:xx` |
| 13:03:57 | **登录成功，进入在线态** |
| 13:03:57 | `trusted_macs.json` 生成 |

**结论**：从"检测到掉线"到"登录成功"为 **4 秒**，优于 §13 中"Wi-Fi 断连恢复 P95 < 15s"的目标。脚本自测报告显示 15.7 秒，是因为它把"轮询 + 每次 `epo status` 的双探针耗时"都算进去了，属于测量口径差异，不是真实恢复耗时。

TOFU 首次信任值已落地，后续登录若网关 MAC 变化会被拒绝并提示。

**待补**：会话自然过期（非手动注销）时的行为；同账号二次登录；密码错误文案。

## 26. M3 图形界面（本地 Web 方案）

**为什么不用 Wails（本轮）**：Wails 需要 Node 工具链并从模块代理拉取依赖，而当前校园网连 GitHub 都不通，构建会被卡死；引入依赖还会让"零依赖单文件"的定位失效。

**本轮做法**：`epo gui` 启动一个只监听 `127.0.0.1` 的本地 HTTP 服务，用系统默认浏览器打开，前端资源用 `go:embed` 打进 exe。

| 设计点 | 说明 |
|---|---|
| 鉴权 | 启动时生成 16 字节随机令牌，只出现在打开的 URL 里；所有 `/api/*` 请求必须带 `X-Epo-Token`，否则 403 |
| 监听范围 | 仅 `127.0.0.1`，端口随机（`:0`），不暴露到局域网 |
| 请求体限制 | `MaxBytesReader` 4 KB，防止异常负载 |
| 不依赖 | 仅标准库；前端为单个 HTML，无 CDN、无框架 |
| 功能 | 状态卡片、立即登录、账号保存（DPAPI）、开机自启开关、日志尾部 |

**冒烟实测**（2026-11-07）：

```
/api/status  → {"state":"online","ssid":"JXAU-Stu","ipv4":"10.100.x.x",
                "gateway":"10.100.0.1","gateway_mac":"xx:xx:xx:xx:xx:xx",
                "random_mac":false,"has_cred":true,"has_trust":true}
/api/autostart → {"enabled":false,"value":""}
无令牌请求 /api/status → 403
```

**后续**：网络允许时可再包一层 Wails 外壳复用同一套前端；托盘仍按 v0.6 设计放在 daemon 侧。

## 27. M3 原生窗口（Wails 落地）

网络恢复后确认：`api.github.com` 可达、`goproxy.cn` 返回 200、本机已有 `D:\Node\node.exe`，因此可以真正上 Wails。

| 项 | 取值 | 说明 |
|---|---|---|
| Wails | **v2.10.2** | v2.17 要求 Go 1.26，本机 1.21 无法满足；v2.10.2 兼容 Go 1.22 |
| 模块代理 | `goproxy.cn` | `proxy.golang.org` 在校园网超时 |
| 前端 | 纯 HTML/CSS/JS，无框架 | `wails.json` 的 `frontend:install` / `frontend:build` 置空，**不依赖 Node/npm** |
| 绑定 | `App.Status / Login / SaveCred / Autostart / SetAutostart / Logs` | 进程内调用，不起本地 HTTP 服务 |
| 产物 | `epo-gui.exe` **9.39 MB** | `-webview2 browser`，复用系统 WebView2 |
| 验证 | 进程启动后存活 4 秒 | 窗口正常创建 |

**与 §26 的关系**：§26 的本地 Web 界面保留为**备选**（无 WebView2 时可用）；原生窗口为标准形态。

**工程影响**：`go.mod` 升到 `go 1.22.0` 并引入 Wails 依赖；`internal/session` 抽出 CLI 与 GUI 共用的探针/参数解析逻辑，避免两套实现漂移。

**界面形态**：参考 SniShaper 的桌面软件布局，采用无边框窗口（`Frameless: true`，1000×680），自绘标题栏（品牌 + 最小化/最大化/关闭，拖动区通过 CSS `--wails-draggable: drag` 声明），左侧 220px 导航（概览 / 账号 / 运行日志 / 关于），底部开机自启开关与版本号。

**深浅双主题**：两套 CSS 变量（`#0d1117` 深色 / `#f6f8fa` 浅色），标题栏右侧太阳/月亮按钮切换；选择写入 `localStorage`，首次启动按 `prefers-color-scheme` 跟随系统；切换时调用 `WindowSetBackgroundColour` 同步窗口底色，避免无边框窗口在着色边缘露出异色。

**注意**：`.gitignore` 中的 `dist/` 必须写成 `/dist/`，否则会连带忽略 `cmd/epo-gui/frontend/dist/index.html`，导致仓库缺少界面源码。
