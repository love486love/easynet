// Package session 提供 CLI 与 GUI 共用的"探活 + 解析门户参数"逻辑。
package session

import (
	"context"
	"errors"
	"fmt"
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

// CampusPlan 是选中校区的唯一登录目标：门户地址、协议与 SSID 都只来自本校区。
// 校区之间硬隔离——选本部不碰东区地址，选东区不碰本部地址。
type CampusPlan struct {
	Campus     string   // dongqu / benbu
	PortalHost string   // 本校区门户主机
	PortalPort int      // 本校区门户端口
	SSIDs      []string // 本校区候选 SSID（按优先级）
	UseRuijie  bool     // 本部锐捷 / 东区 Dr.COM
}

const (
	// defaultBenbuHost 是本部锐捷门户。正常配置会以 AllowedHosts 形式提供，
	// 这里只作为老配置缺少白名单时的兜底。
	defaultBenbuHost = "10.255.240.8"
	benbuPortalPort  = 80
)

// Plan 按用户选择的校区（PreferCampus）给出唯一登录目标。
func Plan(prof config.Profile) CampusPlan {
	campus := prof.Network.PreferCampus
	if campus != config.CampusBenbu {
		campus = config.CampusDongqu
	}
	if campus == config.CampusBenbu {
		host := defaultBenbuHost
		for _, h := range prof.Portal.AllowedHosts {
			if h != "" && !strings.EqualFold(h, prof.Portal.Host) {
				host = h
				break
			}
		}
		return CampusPlan{
			Campus:     campus,
			PortalHost: host,
			PortalPort: benbuPortalPort,
			SSIDs:      prof.SSIDsForCampus(campus),
			UseRuijie:  true,
		}
	}
	return CampusPlan{
		Campus:     campus,
		PortalHost: prof.Portal.Host,
		PortalPort: prof.Portal.Port,
		SSIDs:      prof.SSIDsForCampus(campus),
		UseRuijie:  false,
	}
}

// EndpointForCampus 从探针的 302/劫持页解析登录参数，但只接受本校区门户。
// 另一校区的重定向一律忽略并告警：避免拿着本校区凭据去打对方门户。
func EndpointForCampus(prof config.Profile, ev probe.Evidence) (portal.Endpoint, []string) {
	plan := Plan(prof)
	ep := portal.Endpoint{
		Host:      plan.PortalHost,
		Port:      plan.PortalPort,
		LoginPath: prof.Portal.LoginPath,
		Extra:     map[string]string{},
	}
	for k, v := range prof.Login.ExtraParams {
		ep.Extra[k] = v
	}

	var warns []string

	if ev.Location == "" {
		warns = append(warns, "未拿到门户重定向地址，登录参数可能缺失")
		return ep, warns
	}

	u, err := url.Parse(ev.Location)
	if err != nil {
		warns = append(warns, "门户重定向地址无法解析，已忽略")
		return ep, warns
	}
	if h := u.Hostname(); h != "" && !strings.EqualFold(h, plan.PortalHost) {
		warns = append(warns, fmt.Sprintf("门户重定向指向另一校区（%s），已忽略", h))
		return ep, warns
	}

	if ps := u.Port(); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			ep.Port = n
		}
	}
	// 本部锐捷登录要把 index.jsp 的完整 query 原样透传（双重编码），
	// 东区 Dr.COM 的 userip/usermac/wlanacip 等参数同样取自这里。
	ep.Extra["rawquery"] = u.RawQuery
	ep.Extra["indexpath"] = u.EscapedPath()
	q := u.Query()
	ep.Extra["userip"] = q.Get("userip")
	ep.Extra["usermac"] = q.Get("usermac")
	ep.Extra["wlanacip"] = q.Get("wlanacip")

	// userip/usermac/wlanacip 是东区 Dr.COM 的参数命名；本部锐捷用
	// wlanuserip/mac/nasip 且整串原样透传，为空属正常，不告警。
	if !plan.UseRuijie {
		for _, k := range []string{"userip", "usermac", "wlanacip"} {
			if ep.Extra[k] == "" {
				warns = append(warns, k+" 为空，仍尝试登录")
			}
		}
	}
	return ep, warns
}
