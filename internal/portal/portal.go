// Package portal 定义与具体门户协议无关的公共类型。
// 上层（probe、daemon、cli）只依赖本包，不直接依赖 drcom 等实现。
package portal

import "context"

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
	Code    int    // 见 docs/TECH_DESIGN.md §11 错误码
	Message string // 可直接展示的文案
	RetCode int    // 服务端返回的 ret_code，仅用于诊断
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
