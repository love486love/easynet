package session

import (
	"strings"
	"testing"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/probe"
)

// 东区 Dr.COM 重定向：带显式 801 端口，行为保持不变。
func TestEndpointForCampusDongQu(t *testing.T) {
	prof := config.Default()
	ev := probe.Evidence{
		Location: "http://172.16.121.9:801/eportal/interFace?userip=10.1.2.3&usermac=aabbcc&" +
			"wlanacip=172.16.121.9",
	}
	ep, warns := EndpointForCampus(prof, ev)
	if ep.Host != "172.16.121.9" || ep.Port != 801 {
		t.Fatalf("东区门户应为 172.16.121.9:801，实际 %s:%d", ep.Host, ep.Port)
	}
	if ep.Extra["userip"] != "10.1.2.3" || ep.Extra["usermac"] != "aabbcc" {
		t.Errorf("东区参数解析错误: %v", ep.Extra)
	}
	if ep.Extra["rawquery"] == "" {
		t.Error("应保存原始 query string")
	}
	if len(warns) != 0 {
		t.Errorf("不应有告警: %v", warns)
	}
}

// 本部锐捷重定向：必须已选择本部，地址落 10.255.240.8:80。
func TestEndpointForCampusBenbu(t *testing.T) {
	prof := config.Default()
	prof.Network.PreferCampus = config.CampusBenbu
	const q = "wlanuserip=524ff8577378bf2152bc2e49eee4b035&wlanacname=c75799cc&ssid=" +
		"&nasip=fa03676e&mac=92c2115b&t=wireless-v2&url=45f7582d"
	ev := probe.Evidence{
		Location: "http://10.255.240.8/eportal/index.jsp?" + q,
	}
	ep, warns := EndpointForCampus(prof, ev)
	if ep.Host != "10.255.240.8" {
		t.Fatalf("本部门户 host 错误: %s", ep.Host)
	}
	if ep.Port != 80 {
		t.Fatalf("本部门户无端口应落 80，实际 %d", ep.Port)
	}
	if ep.Extra["rawquery"] != q {
		t.Errorf("rawquery 应原样保存，实际 %q", ep.Extra["rawquery"])
	}
	if ep.Extra["indexpath"] != "/eportal/index.jsp" {
		t.Errorf("indexpath 错误: %q", ep.Extra["indexpath"])
	}
	if len(warns) != 0 {
		t.Errorf("不应有告警: %v", warns)
	}
}

// 选本部时，东区的重定向必须被忽略：绝不拿本部凭据去打东区门户。
func TestEndpointForCampusIgnoresDongQuWhenBenbu(t *testing.T) {
	prof := config.Default()
	prof.Network.PreferCampus = config.CampusBenbu
	ev := probe.Evidence{
		Location: "http://172.16.121.9:801/eportal/interFace?userip=10.1.2.3&usermac=aabbcc",
	}
	ep, warns := EndpointForCampus(prof, ev)
	if ep.Host != "10.255.240.8" || ep.Port != 80 {
		t.Fatalf("选本部时门户必须锁定 10.255.240.8:80，实际 %s:%d", ep.Host, ep.Port)
	}
	if ep.Extra["rawquery"] != "" || ep.Extra["userip"] != "" {
		t.Errorf("另一校区的参数必须丢弃: %v", ep.Extra)
	}
	if !hasWarning(warns, "另一校区") {
		t.Errorf("应有跨校区告警，实际 %v", warns)
	}
}

// 选东区时，本部的重定向必须被忽略。
func TestEndpointForCampusIgnoresBenbuWhenDongQu(t *testing.T) {
	prof := config.Default()
	prof.Network.PreferCampus = config.CampusDongqu
	ev := probe.Evidence{
		Location: "http://10.255.240.8/eportal/index.jsp?wlanuserip=1.2.3.4&nasip=aa",
	}
	ep, warns := EndpointForCampus(prof, ev)
	if ep.Host != prof.Portal.Host || ep.Port != prof.Portal.Port {
		t.Fatalf("选东区时门户必须保持 %s:%d，实际 %s:%d",
			prof.Portal.Host, prof.Portal.Port, ep.Host, ep.Port)
	}
	if ep.Extra["rawquery"] != "" {
		t.Errorf("另一校区的参数必须丢弃: %v", ep.Extra)
	}
	if !hasWarning(warns, "另一校区") {
		t.Errorf("应有跨校区告警，实际 %v", warns)
	}
}

// 无 Location：按选择给默认门户并告警，不跨校区兜底。
func TestEndpointForCampusNoLocation(t *testing.T) {
	prof := config.Default()
	prof.Network.PreferCampus = config.CampusBenbu
	ep, warns := EndpointForCampus(prof, probe.Evidence{})
	if ep.Host != "10.255.240.8" || ep.Port != 80 {
		t.Fatalf("选本部时应回退本部门户 10.255.240.8:80，实际 %s:%d", ep.Host, ep.Port)
	}
	if len(warns) == 0 {
		t.Error("缺 Location 应有告警")
	}
}

// Plan 的 SSID 列表必须只含选中校区，避免自动切换串校区。
func TestPlanSSIDIsolation(t *testing.T) {
	prof := config.Default()

	prof.Network.PreferCampus = config.CampusBenbu
	bb := Plan(prof)
	if !bb.UseRuijie || bb.PortalHost != "10.255.240.8" || bb.PortalPort != 80 {
		t.Fatalf("本部计划错误: %+v", bb)
	}
	for _, s := range bb.SSIDs {
		if strings.EqualFold(s, prof.Network.SSID) && !strings.EqualFold(s, "JXAU") {
			t.Errorf("本部计划不应包含东区 SSID %q: %v", s, bb.SSIDs)
		}
	}

	prof.Network.PreferCampus = config.CampusDongqu
	dq := Plan(prof)
	if dq.UseRuijie || dq.PortalHost != prof.Portal.Host || dq.PortalPort != prof.Portal.Port {
		t.Fatalf("东区计划错误: %+v", dq)
	}
	for _, s := range dq.SSIDs {
		for _, extra := range prof.Network.ExtraSSIDs {
			if strings.EqualFold(s, extra) {
				t.Errorf("东区计划不应包含本部 SSID %q: %v", s, dq.SSIDs)
			}
		}
	}
}

func hasWarning(warns []string, substr string) bool {
	for _, w := range warns {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
