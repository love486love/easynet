# epo

校园网门户自动认证客户端（Windows 10/11 x64）。

连上校园 Wi-Fi 后自动完成 Dr.COM ePortal 网页认证，无需再打开浏览器输入账号密码。

> 当前状态：**M3 已完成**。协议层、判定层、凭据、守护循环（含系统托盘）、原生窗口 GUI、界面与守护联动均已实现；真机掉线恢复实测 4 秒。

## 安装

1. 从 [Releases](https://github.com/love486love/epo-2026/releases) 下载 `epo-windows-x64.zip`
2. 解压到固定目录（例如 `C:\epo`），三个 exe 必须放在同一目录
3. 双击 `epo-gui.exe`，在「账号」页填写学号、密码、运营商线路并保存
4. 概览页点「启动守护」；需要开机自动重连时，打开左下角「开机自启」
5. 关闭界面后守护仍在运行；托盘图标可查看状态、打开界面或退出

### 升级

下载新版本 zip，先从托盘选择「退出」，再覆盖三个 exe。配置与凭据保存在 `%LOCALAPPDATA%\epo`，升级不会丢失。

### 卸载

1. 托盘选择「退出」，并在界面关闭「开机自启」
2. 删除解压目录
3. 删除 `%LOCALAPPDATA%\epo`（凭据、日志、状态快照）

### 已知限制

- Windows 10/11 x64，需要系统 WebView2（Win11 自带；Win10 通常随 Edge 安装）
- 无代码签名，首次运行 SmartScreen 会提示「未知发布者」
- 无自动更新，升级需手动覆盖
- 同一账号支持多设备同时在线（2026-11-07 实测）
- 若系统缺少 WebView2，改用 `epo.exe`（CLI）或 `epo gui`（本地网页版）

## 命令行

```bash
epo status          # 查看在线状态
epo doctor          # 诊断：本机网络 + 各探针证据 + 网关参数（已脱敏）
epo login -cred c.json        # 登录一次（凭据文件不入库）
epo save-cred -cred c.json    # 用 DPAPI 加密保存凭据
epo daemon          # 常驻守护：掉线自动重连
epo-daemon.exe      # 无控制台守护 + 系统托盘（开机自启指向它）
epo autostart install|remove|status   # 开机自启（HKCU\Run，无需管理员）
epo-gui.exe         # 原生窗口图形界面（Wails + WebView2）
epo gui             # 轻量备选：本地网页界面（无 WebView2 时可用）
```

## 特性

- 连网自动认证、断线自动恢复（真机实测 4.1 秒恢复）
- 常驻守护进程（含托盘），关闭界面后继续工作
- 原生窗口界面，支持深色 / 浅色主题
- 凭据使用 Windows DPAPI 加密，仅本机本账户可解密
- 网关 MAC 首次信任（TOFU），网关变化时拒绝自动登录
- 探针只读响应头，单次流量 ≤ 10 KB

## 常见问题

**关闭界面后还会自动重连吗？**
会。守护 `epo-daemon.exe` 是独立进程；托盘图标可查看状态、打开界面或退出。

**杀毒软件报毒 / SmartScreen 提示未知发布者？**
程序没有代码签名，Go 编译的 exe 常被误判。可核对 Release 附带的 SHA256，或自行从源码构建；确认后加入白名单。

**手机和电脑能同时用同一学号吗？**
可以。2026-11-07 实测门户无单终端限制。

**密码存在哪里？安全吗？**
`%LOCALAPPDATA%\epo\secret.bin`，用 Windows DPAPI 以当前用户身份加密；换账户或换电脑都无法解密。

**为什么不需要管理员权限？**
开机自启写入 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，只影响当前用户。

**界面显示「后台守护未运行」？**
确认 `epo-daemon.exe` 与 `epo-gui.exe` 在同一目录，点「启动守护」后查看守护条上的失败原因。

**支持其他学校吗？**
不支持。门户地址、协议参数与 SSID 均为江西农业大学专用。

## 隐私

- 所有数据保存在本机 `%LOCALAPPDATA%\epo`
- 不采集、不上报任何信息，无遥测
- 只访问学校门户与探针地址（学校主页、百度）

## 文档

- [技术设计](docs/TECH_DESIGN.md)
- [M0 实测报告](docs/M0-REPORT.md)
- [发布说明](docs/RELEASE_NOTES.md)

## 免责声明

本项目为非官方工具，与江西农业大学及广州热点软件科技股份有限公司（Dr.COM）无任何关联。

- 仅用于使用者本人在校园网内的授权账号
- 不提供多设备共享、绕过计费或破解功能
- 校园网凭据在网关侧以明文 HTTP 传输，这是学校网关的设计限制，非本工具可改变
- 学校调整认证策略（验证码、无感知认证、MAC 白名单等）可能导致本工具失效

## 许可证

MIT
