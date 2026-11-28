// Package portal 定义与具体门户协议无关的公共类型。
// 上层（probe、daemon、cli）只依赖本包，不直接依赖 drcom 等实现。
package portal

import (
	"context"
	"regexp"
	"strings"
)

// maxHijackBody 是提取跳转地址时允许检查的正文上限。
// 真页面（几十 KB）不可能是 AC 劫持页，超过上限直接放弃。
const maxHijackBody = 8 << 10

// reIndexJSP 匹配 AC 劫持页里的 index.jsp 跳转地址。
// 本部锐捷实测：未认证时返回 200 + 511 字节 JS 跳转页，
// 地址以单引号包裹的绝对 URL 出现在正文；部分部署走 302 Location。
var reIndexJSP = regexp.MustCompile(`https?://[^'\s<>"]+index\.jsp\?[^'\s<>"]+`)

// FindIndexJSP 从响应头 Location 或正文中提取 index.jsp 跳转地址。
// 只做文本提取，不发请求，供 probe 与门户客户端共用。
func FindIndexJSP(location string, body []byte) string {
	if m := reIndexJSP.FindString(location); m != "" {
		return m
	}
	if len(body) > maxHijackBody {
		return ""
	}
	return reIndexJSP.FindString(string(body))
}

// State 表示当前上网认证状态。
type State int

const (
	// StateUnknown 表示无法判定（探针不可用等）。
	StateUnknown State = iota
	// StateOffline 表示已连上网络但未认证。
	StateOffline
	// StateOnline 表示已认证，可访问外网。
	StateOnline
)

func (s State) String() string {
	switch s {
	case StateOffline:
		return "offline"
	case StateOnline:
		return "online"
	default:
		return "unknown"
	}
}

// Endpoint 描述一个门户的入口。全部为协议无关字段，
// 网关私有参数放在 Extra 中，避免污染通用接口。
type Endpoint struct {
	Host       string            // 门户主机，例如 172.16.121.9
	Port       int               // 门户端口，例如 801
	LoginPath  string            // 登录路径，例如 /eportal/portal/login
	LogoutPath string            // 登出路径（未验证，当前不使用）
	Extra      map[string]string // 网关附加参数
}

// Credential 是用户凭据。仅在内存中存在，不得写日志。
type Credential struct {
	Username string // 学号
	Password string // 密码
	Carrier  string // 运营商：cmcc / ctcc / cucc / campus
}

// Result 是一次认证操作的结构化结果。
type Result struct {
	OK      bool   // 是否成功
	Code    int    // 错误码，见本文件下方的约定
	Message string // 可直接展示的文案
	RetCode int    // 服务端返回的 ret_code，仅用于诊断
}

// RejectKind 是登录被拒的原因分类。网关只返回文案、没有稳定的错误码，
// 因此按关键词归类；判不出来的一律按普通失败处理（照常退避重试）。
type RejectKind int

const (
	// RejectUnknown 表示未归类，按普通失败处理。
	RejectUnknown RejectKind = iota
	// RejectTimePolicy 表示当前不在允许上网的时间段（本科生限时策略）。
	RejectTimePolicy
	// RejectBadCredential 表示密码错误或账号不存在。
	RejectBadCredential
	// RejectLocked 表示账号被锁定、冻结或欠费停机。
	RejectLocked
	// RejectConcurrency 表示账号并发/终端数超限。
	RejectConcurrency
)

func (k RejectKind) String() string {
	switch k {
	case RejectTimePolicy:
		return "time_policy"
	case RejectBadCredential:
		return "credential"
	case RejectLocked:
		return "locked"
	case RejectConcurrency:
		return "concurrency"
	default:
		return ""
	}
}

// 关键词表。只用于归类重试策略，不改变"登录失败"这一事实。
var (
	timePolicyWords = []string{
		"上网时间", "上网时段", "非上网时间", "不在允许", "不在服务时间",
		"禁止上网", "不允许上网", "时间策略",
	}
	lockedWords = []string{
		"锁定", "冻结", "欠费", "停机", "停用", "暂停使用", "已报停",
	}
	badCredentialWords = []string{
		"密码错误", "密码不正确", "密码有误", "用户不存在", "账号不存在",
		"用户名或密码", "账号或密码",
	}
	concurrencyWords = []string{
		"同时在线", "并发登录", "超过最大", "在线设备", "终端数",
	}
)

// ClassifyReject 按网关返回的文案归类拒绝原因。
// 判断只看网关返回：账号能登录成功（能上网）就说明不受限时策略约束，
// 不按当前时间猜测。
func ClassifyReject(res Result) RejectKind {
	if res.OK || res.Message == "" {
		return RejectUnknown
	}
	switch {
	case containsAny(res.Message, timePolicyWords):
		return RejectTimePolicy
	case containsAny(res.Message, lockedWords):
		return RejectLocked
	case containsAny(res.Message, badCredentialWords):
		return RejectBadCredential
	case containsAny(res.Message, concurrencyWords):
		return RejectConcurrency
	default:
		return RejectUnknown
	}
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Portal 是一种门户协议的实现。
type Portal interface {
	// Name 返回协议标识，例如 drcom-eportal。
	Name() string
	// Login 使用给定凭据完成认证。
	Login(ctx context.Context, ep Endpoint, c Credential) (Result, error)
	// Logout 注销当前会话。接口保留，但当前实现未经实测，调用方需自行降级。
	Logout(ctx context.Context, ep Endpoint) error
}
