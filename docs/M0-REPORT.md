# M0 实测报告

| 项目 | 内容 |
|---|---|
| 时间 | 2026-11-02 10:16:33 |
| 网络状态 | `JXAU-Stu` 已连接、**未认证**（已注销） |
| 本机 IP / 网关 | `10.x.x.x` / `10.100.0.1` |
| 网关 MAC | `AA-BB-CC-DD-EE-FF` |
| 采集方式 | 本机离线运行 `tools/m0-probe.ps1`，无需联网 |

---

## 1. 探针结果（核心结论）

三个地址在未认证状态下**返回完全一致的 302**：

| 探针 | 状态 | Location |
|---|---|---|
| `http://www.jxau.edu.cn/?epo=…` | 302 | `http://172.16.121.9/a79.htm?userip=10.x.x.x&wlanacname=&wlanacip=172.16.x.x&usermac=opaque…` |
| `http://www.baidu.com/?epo=…` | 302 | 同上 |
| `http://captive.apple.com/generate_204` | 302 | 同上 |

响应头关键字段：

```
Server: NetEngine Server 1.0
Content-Type: text/html
Location: http://172.16.121.9/a79.htm?userip=…&wlanacip=172.16.x.x&usermac=…
```

响应体是标准 WISPA 门户拦截 XML：

```xml
<WISPAccessGatewayParam ...>
  <Proxy>
    <MessageType>110</MessageType>
    <NextURL>http://172.16.121.9/a79.htm?...</NextURL>
```

**结论**：判定规则可定稿为「`Location` 主机 ≠ 探针自身域名 → 未认证」。主备双探针结论一致，可交叉验证。

补充：`generate_204` 在**在线**时曾返回 200 伪成功（CDN 缓存），**离线**时正常 302。它本身可用，但为防止学校后续加白名单，仍不作默认探针。

---

## 2. 登录前参数（确认可由重定向获取）

从 302 的 `Location` 中直接拿到三项前置参数，无需额外接口：

```
userip    = 10.x.x.x
wlanacip  = 172.16.x.x
usermac   = opaque-usermac-xxxx
```

这与 HAR 抓包中的登录请求参数一致，说明**登录前置步骤成立，不会死锁**（本次即为"注销后"状态）。

---

## 3. 失败响应样本

用假账号 `00000000000@cmcc` + 无效密码请求 `/eportal/portal/login`：

```json
dr1003({"result":0,"msg":"账号不存在","ret_code":1});
```

- `result`：1 成功 / 0 失败
- `msg`：可直接展示的中文文案
- `ret_code`：程序分支可用

**带参数与不带参数请求返回完全相同**，说明该分支不校验 `wlan_user_ip` / `wlan_user_mac`。

仍缺的样本：密码错误、账号锁定/欠费、单终端限制。这三类暂不做细分，Code 2 透传 `msg`。

---

## 4. `chkstatus` 不可用于状态判定

未认证时：

```json
dr1002({"result":0,"v46ip":"10.x.x.x","v4serip":"172.16.121.9","hidn":-5, ...})
```

而在线抓包样本中同样是 `result:0`。两者无法区分，**因此不能作为在线判定依据**，仅作旁路记录（与 ADR-007 一致）。

---

## 5. 旧版表单接口已废弃

```
GET http://172.16.121.9:801/eportal/?c=ACSetting&a=Login&url=drappall
→ 200, text/html, <title>EPortal</title>（Vue 页面）
```

返回的是 EPortal 的 Vue 应用首页，不是可提交的登录表单。**兼容分支删除**，不实现回退，避免一次失败触发双请求。

---

## 6. 对设计的直接影响

| 项 | 结论 |
|---|---|
| §6.1 判定算法 | 定稿：Location 主机 ≠ 探针域名 → 未认证；补 WISPA 特征串 |
| §6.2 TOFU | 网关 MAC 可读取，方案可行 |
| §6.3 登录前置 | 参数来源确认为 302 的 Location |
| §6.3 兼容分支 | 删除 |
| §11 错误码 | 保留 `ret_code`，暂不细分 |
| ADR-007 | 维持：`chkstatus` 只记录不判定 |

---

## 6.5 补充：在线态实测（2026-11-02，登录状态下）

| 探针 | 无 Range | 带 Range |
|---|---|---|
| `http://www.jxau.edu.cn/?epo=…` | 200，`Content-Length 91461`，无跳转 | **206**，`Content-Length 1024` |
| `http://www.baidu.com/?epo=…` | 302 → `https://www.baidu.com/?epo=…`（同站） | 302（同左） |
| `http://captive.apple.com/generate_204` | 200，`Server: nginx`，68 字节 | 200（同左） |

**结论**：

1. 为省流量加的 `Range` 会让支持静态分段的服务器返回 **206**，判定必须按 **2xx（含 206）**，不能用"200/204"。
2. 百度在线时 302 到 HTTPS 且**主机不变**，说明"3xx 同站 = 在线"成立；但仍应支持 www 与非 www 视为同站。
3. `generate_204` 在线 200、离线 302，行为与状态一致。

---

## 7. 仍未完成

1. 同账号二次登录响应（是否存在单终端限制）
2. 密码错误 / 账号锁定 / 欠费的真实失败文案
3. Wi-Fi 随机硬件地址开启后，`usermac` 是否变化

以上均为**增强项**，不影响 M1 开工。
