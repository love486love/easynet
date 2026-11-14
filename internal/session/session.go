// Package session 提供 CLI 与 GUI 共用的"探活 + 解析门户参数"逻辑。
package session

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/portal"
	"github.com/love486love/easynet/internal/probe"
)

// ProbeOnce 逐个探针判定：任意一个在线即在线，全部失败才视为离线。
func ProbeOnce(ctx context.Context, prof config.Profile) (portal.State, probe.Evidence, error) {
	c := probe.New()

	var firstOffline *probe.Evidence
	var errs []string

	for _, p := range prof.Network.Probes {
		state, ev, err := c.Check(ctx, p.URL, prof.Network.ProbeHosts, p.Expect)
		if err != nil {
			// 所有探针的错误都要留痕：只留最后一个会掩盖
			// 「校内探针好使、外网探针超时」这类关键排障信息。
			errs = append(errs, p.URL+": "+err.Error())
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
	if len(errs) > 0 {
		return portal.StateUnknown, probe.Evidence{}, errors.New(strings.Join(errs, "; "))
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
		} else if ep.Host != prof.Portal.Host {
			// 本部锐捷的重定向不带端口（http://10.255.240.8/eportal/...）：
			// 门户换了主机就按 scheme 默认 80，沿用配置里的 801 会打错端口。
			ep.Port = 80
		}
		// 本部锐捷登录要把 index.jsp 的完整 query 原样透传（双重编码），
		// 东区 Dr.COM 的 userip/usermac/wlanacip 等参数同样取自这里。
		ep.Extra["rawquery"] = u.RawQuery
		ep.Extra["indexpath"] = u.EscapedPath()
		q := u.Query()
		ep.Extra["userip"] = q.Get("userip")
		ep.Extra["usermac"] = q.Get("usermac")
		ep.Extra["wlanacip"] = q.Get("wlanacip")
	}

	// userip/usermac/wlanacip 是东区 Dr.COM 的参数命名；本部锐捷用
	// wlanuserip/mac/nasip 且整串原样透传，为空属正常，不告警。
	if !strings.Contains(ep.Extra["indexpath"], "index.jsp") {
		for _, k := range []string{"userip", "usermac", "wlanacip"} {
			if ep.Extra[k] == "" {
				warns = append(warns, k+" 为空，仍尝试登录")
			}
		}
	}
	return ep, warns
}
