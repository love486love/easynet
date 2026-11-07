// Package session 提供 CLI 与 GUI 共用的"探活 + 解析门户参数"逻辑。
package session

import (
	"context"
	"net/url"
	"strconv"

	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/probe"
)

// ProbeOnce 逐个探针判定：任意一个在线即在线，全部失败才视为离线。
func ProbeOnce(ctx context.Context, prof config.Profile) (portal.State, probe.Evidence, error) {
	c := probe.New()

	var firstOffline *probe.Evidence
	var lastErr error

	for _, p := range prof.Network.Probes {
		state, ev, err := c.Check(ctx, p.URL, prof.Network.ProbeHosts, p.Expect)
		if err != nil {
			lastErr = err
			continue
		}
		if state == portal.StateOnline {
			return state, ev, nil
		}
		if state == portal.StateOffline && firstOffline == nil {
			e := ev
			firstOffline = &e
		}
	}

	if firstOffline != nil {
		return portal.StateOffline, *firstOffline, nil
	}
	if lastErr != nil {
		return portal.StateUnknown, probe.Evidence{}, lastErr
	}
	return portal.StateUnknown, probe.Evidence{}, nil
}

// EndpointFromEvidence 从 302 的 Location 中解析网关地址与登录参数。
// 参数缺失只告警不阻断：同类项目实测这些字段并非认证必填项。
func EndpointFromEvidence(prof config.Profile, ev probe.Evidence) (portal.Endpoint, []string) {
	ep := portal.Endpoint{
		Host:      prof.Portal.Host,
		Port:      prof.Portal.Port,
		LoginPath: prof.Portal.LoginPath,
		Extra:     map[string]string{},
	}
	for k, v := range prof.Login.ExtraParams {
		ep.Extra[k] = v
	}

	var warns []string

	if ev.Location == "" {
		warns = append(warns, "未拿到门户重定向地址，网关参数可能缺失")
	} else if u, err := url.Parse(ev.Location); err == nil {
		if h := u.Hostname(); h != "" {
			ep.Host = h
		}
		if ps := u.Port(); ps != "" {
			if n, err := strconv.Atoi(ps); err == nil {
				ep.Port = n
			}
		}
		q := u.Query()
		ep.Extra["userip"] = q.Get("userip")
		ep.Extra["usermac"] = q.Get("usermac")
		ep.Extra["wlanacip"] = q.Get("wlanacip")
	}

	for _, k := range []string{"userip", "usermac", "wlanacip"} {
		if ep.Extra[k] == "" {
			warns = append(warns, k+" 为空，仍尝试登录")
		}
	}
	return ep, warns
}
