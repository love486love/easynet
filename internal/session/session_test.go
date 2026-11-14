package session

import (
	"testing"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/probe"
)

// 东区 Dr.COM 重定向：带显式 801 端口，行为保持不变。
func TestEndpointFromEvidenceDongQu(t *testing.T) {
	prof := config.Default()
	ev := probe.Evidence{
		Location: "http://172.16.121.9:801/eportal/interFace?userip=10.1.2.3&usermac=aabbcc&" +
			"wlanacip=172.16.121.9",
	}
	ep, warns := EndpointFromEvidence(prof, ev)
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

// 本部锐捷重定向：无端口（:8001 之外的默认 http）+ index.jsp 完整 query。
func TestEndpointFromEvidenceBenbu(t *testing.T) {
	prof := config.Default()
	const q = "wlanuserip=524ff8577378bf2152bc2e49eee4b035&wlanacname=c75799cc&ssid=" +
		"&nasip=fa03676e&mac=92c2115b&t=wireless-v2&url=45f7582d"
	ev := probe.Evidence{
		Location: "http://10.255.240.8/eportal/index.jsp?" + q,
	}
	ep, warns := EndpointFromEvidence(prof, ev)
	if ep.Host != "10.255.240.8" {
		t.Fatalf("本部门户 host 错误: %s", ep.Host)
	}
	// 关键：换门户且 Location 无端口时不能沿用配置里的 801。
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

// 无 Location：保留配置门户并告警（原行为）。
func TestEndpointFromEvidenceNoLocation(t *testing.T) {
	prof := config.Default()
	ep, warns := EndpointFromEvidence(prof, probe.Evidence{})
	if ep.Host != prof.Portal.Host || ep.Port != prof.Portal.Port {
		t.Fatalf("应保留配置门户 %s:%d，实际 %s:%d",
			prof.Portal.Host, prof.Portal.Port, ep.Host, ep.Port)
	}
	if len(warns) == 0 {
		t.Error("缺 Location 应有告警")
	}
}
