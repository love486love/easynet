# easynet

[![release](https://img.shields.io/github/v/release/love486love/easynet?label=release)](https://github.com/love486love/easynet/releases)
[![ci](https://github.com/love486love/easynet/actions/workflows/ci.yml/badge.svg)](https://github.com/love486love/easynet/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

江西农业大学校园网（JXAU-Stu）自动认证客户端，Windows 10 / 11 x64。

连上校园 Wi-Fi 后自动完成 Dr.COM ePortal 网页认证，断线自动重连；关闭窗口后仍在后台工作，无需再打开浏览器输入账号密码。

> 当前版本 **v0.3.0**。真机实测：会话过期后 **4.1 秒**恢复在线。

## 特性

- **自动认证**：连上 `JXAU-Stu` 后自动登录，掉线后几秒内重连
- **自动连接 Wi-Fi**：开启后若没连着校园网，守护会自动连回 `JXAU-Stu`（默认关闭）
- **单文件**：界面与后台守护是同一个 `easynet.exe`（守护以 `--daemon` 参数运行）
- **守护默认开启**：开关状态持久保存，关掉界面、从托盘退出、重新打开都保持
- **多账号**：可保存多个学号并随时切换，切换后守护自动用新账号重启
- **凭据安全**：Windows DPAPI 加密，仅本机本账户可解密
- **防劫持**：首次记录网关 MAC（TOFU），网关变化时拒绝自动登录
- **原生界面**：无边框自绘标题栏，深色 / 浅色双主题，内置新手引导
- **无需管理员**：开机自启写入 `HKCU\Run`

## 下载

两个包功能完全一样，区别只在安装方式：

| 包 | 适合谁 | 怎么卸载 |
|---|---|---|
| `easynet-windows-x64.zip`（绿色版） | 不想安装、想放任意目录或 U 盘 | 运行目录里的 `uninstall.bat` |
| `easynet-setup-*.exe`（安装版） | 想从「设置 → 应用」正常卸载 | 应用列表卸载，或运行 `unins000.exe` |

数据保存在 `%LOCALAPPDATA%\easynet`，运行日志写在程序目录的 `log` 文件夹。

## 快速开始

1. 解压 / 安装后双击 `easynet.exe`
2. 「账号」页填写学号、密码、运营商线路并保存
3. 概览页的「守护」开关默认就是打开的；需要开机自动重连时，打开左下角「开机自启」
4. 想让软件在没连校园网时自动连回来，打开左下角「自动连接 Wi-Fi」（默认关闭）
5. 关闭窗口后守护仍在运行，托盘图标可查看状态、打开界面或退出

## 系统要求

- Windows 10 / 11 x64
- Microsoft Edge WebView2（Win11 自带；Win10 通常随 Edge 安装）。缺失时程序会提示安装
- 江西农业大学 `JXAU-Stu` 校园网

### 升级

先从托盘选择「退出」，再用新版本覆盖程序目录里的 `easynet.exe`。配置与凭据保存在 `%LOCALAPPDATA%\easynet`，升级不会丢失。开过「开机自启」的话，升级后建议关一次再打开，让自启命令指向新 exe。

### 卸载

- **绿色版**：运行目录里的 `uninstall.bat`（自动关自启、停守护、删数据），然后删除目录
- **安装版**：在「设置 → 应用 → 已安装的应用」里卸载，或运行安装目录的 `unins000.exe`

手动步骤：托盘「退出」→ 关闭「开机自启」→ 删除程序目录 → 删除 `%LOCALAPPDATA%\easynet`

## 常见问题

**关闭界面后还会自动重连吗？**
会。守护是独立的 `easynet.exe --daemon` 进程，托盘图标可查看状态、打开界面或退出。

**「自动连接 Wi-Fi」会把我手动连的网络切走吗？**
开关打开时，只要当前不是 `JXAU-Stu`，守护就会尝试连回校园网；如果想临时用别的网络，把开关关掉即可。软件不会开启或关闭无线网卡本身。

**界面显示「后台守护未运行」？**
确认「守护」开关是打开的，再看守护条上的失败原因；也可以在「运行日志」页导出日志。

**密码存在哪里？安全吗？**
`%LOCALAPPDATA%\easynet\accounts.bin`，用 Windows DPAPI 以当前用户身份加密；换账户或换电脑都无法解密。

**杀毒软件报毒 / SmartScreen 提示未知发布者？**
程序没有代码签名，Go 编译的 exe 常被误判。可核对 Release 附带的 SHA256，或自行从源码构建；确认后加入白名单。

**手机和电脑能同时用同一学号吗？**
可以。2026-11-07 实测门户无单终端限制。

**支持其他学校吗？**
不支持。门户地址、协议参数与 SSID 均为江西农业大学专用。

## 从源码构建

需要 Go 1.22 与 [Wails v2.10.2](https://wails.io)：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
cd cmd/easynet
wails build -platform windows/amd64 -webview2 browser -ldflags "-s -w"
```

产物在 `cmd/easynet/build/bin/easynet.exe`。前端是手写 HTML/CSS/JS，构建过程不调用 Node。

## 项目结构

```
cmd/easynet/          界面 + 守护（--daemon）
internal/
  portal/drcom/       Dr.COM ePortal 协议实现
  probe/              探针与在线判定
  daemon/ guard/      守护状态机与四项校验
  creds/              DPAPI 凭据存储
  trust/              网关 MAC 信任（TOFU）
  config/ state/      配置与状态快照
packaging/            Inno Setup 脚本与绿色版卸载脚本
docs/                 技术设计、实测报告、发布说明
```

## 文档

- [技术设计](docs/TECH_DESIGN.md)
- [M0 实测报告](docs/M0-REPORT.md)
- [发布说明](docs/RELEASE_NOTES.md)

## 隐私

- 所有数据保存在本机 `%LOCALAPPDATA%\easynet`，日志写在程序目录的 `log`
- 不采集、不上报任何信息，无遥测
- 只访问学校门户与探针地址（学校主页、百度）

## 免责声明

本项目为非官方工具，与江西农业大学及广州热点软件科技股份有限公司（Dr.COM）无任何关联。

- 仅用于使用者本人在校园网内的授权账号
- 不提供多设备共享、绕过计费或破解功能
- 校园网凭据在网关侧以明文 HTTP 传输，这是学校网关的设计限制，非本工具可改变
- 学校调整认证策略（验证码、无感知认证、MAC 白名单等）可能导致本工具失效

## 许可证

MIT
