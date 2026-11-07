// Package probe 负责在线状态判定与探针请求。
// 判定逻辑是纯函数，便于用真实样本做回归测试。
package probe

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/love486love/easynet/internal/portal"
)

// Evidence 是判定所需的最小证据集。
// 只依赖状态码、少量响应头与最多 8 KB 正文，不下载整页。
type Evidence struct {
	StatusCode int
	Location   string
	Server     string
	Body       []byte
}

// 拦截页特征串，仅在未配置 expect 时作为兜底判据。
var loginMarkers = [][]byte{
	[]byte("WISPAccessGatewayParam"),
	[]byte("MessageType>110"),
	[]byte("DDDDD"),
	[]byte("upass"),
	[]byte("Dr.COMWebLogin"),
	[]byte("ACSetting"),
	[]byte(`<input type="password"`),
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

// sameSite 判断 host 是否属于 allowed 中的任意一个站点。
// 允许子域与父域互认（www.x.edu.cn 与 x.edu.cn 视为同站）。
func sameSite(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

func looksLikePortal(body []byte) bool {
	for _, m := range loginMarkers {
		if bytes.Contains(body, m) {
			return true
		}
	}
	return false
}

// Judge 依据 docs/TECH_DESIGN.md §6.1 判定在线状态。
//
// expect 是探针地址在正常（已认证）状态下必然出现的正文片段。
// 只要配置了 expect，2xx 就必须命中它才算在线——这是为了防住
// "网关注入登录页但返回 200" 的透明劫持：只看状态码或只看
// "不像登录页" 都可能误判为在线。
func Judge(probeURL string, allowedHosts []string, expect string, ev Evidence) portal.State {
	probeHost := hostOf(probeURL)

	switch {
	case ev.StatusCode >= 300 && ev.StatusCode < 400:
		if ev.Location == "" {
			return portal.StateOffline
		}
		locHost := hostOf(ev.Location)
		if locHost == "" {
			return portal.StateOffline
		}
		// 到达源站才会跳转到自己站点；跳到门户则说明被拦截。
		if locHost == probeHost || sameSite(locHost, allowedHosts) {
			return portal.StateOnline
		}
		return portal.StateOffline

	case ev.StatusCode >= 200 && ev.StatusCode < 300:
		if expect != "" {
			if bytes.Contains(bytes.ToLower(ev.Body), []byte(strings.ToLower(expect))) {
				return portal.StateOnline
			}
			return portal.StateOffline
		}
		// 未配置 expect 时退回特征排除法，判据弱一档。
		if looksLikePortal(ev.Body) {
			return portal.StateOffline
		}
		return portal.StateOnline

	case ev.StatusCode >= 500:
		// 服务器错误不是断网，也不是在线，交由另一个探针决定。
		return portal.StateUnknown
	}

	return portal.StateOffline
}
