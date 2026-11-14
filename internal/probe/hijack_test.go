package probe

import (
	"strings"
	"testing"
)

// 2026-11 本部真机实测的劫持页原文（511 字节，截取关键结构）。
const realHijackBody = `<script>top.self.location.href='http://10.255.240.8/eportal/index.jsp?wlanuserip=524ff8577378bf21a3aff192fb2e5876&wlanacname=c75799cc3430d0793b0dd9e899ae5f37&ssid=&nasip=fa03676e2aa64ff61b306df8b3ec0240&snmpagentip=&mac=92c2115b1e31dd744ad4ca22d48536c5&t=wireless-v2&url=259b2fe62893eec6a8748701607c9cea87a596a3918cfa27&apmac=&nasid=c75799cc3430d0793b0dd9e899ae5f37&vid=3348ed7a7c3f131e&port=ee9d467ba3669e0d&nasportid=05133cabf902a9b2c66024ac743bc1721111a2a9a57844b8913fb5fb2add8761900f3ff75f28586a'</script>`

func TestHijackRedirect(t *testing.T) {
	got := hijackRedirect([]byte(realHijackBody))
	wantPrefix := "http://10.255.240.8/eportal/index.jsp?wlanuserip=524ff857"
	if len(got) < 40 || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("应提取完整跳转 URL，实际 %q", got)
	}
	if !strings.Contains(got, "nasportid=05133cab") {
		t.Errorf("query 应完整保留到 nasportid，实际 %q", got)
	}

	// 普通页面（无跳转）与超大正文都不应误判。
	if hijackRedirect([]byte(`<html><a href="/eportal/index.jsp">portal</a></html>`)) != "" {
		t.Error("相对路径/普通链接不应命中绝对跳转正则")
	}
	if hijackRedirect(make([]byte, maxBody+1)) != "" {
		t.Error("超大正文不应视为劫持页")
	}
	if hijackRedirect(nil) != "" {
		t.Error("空正文应返回空")
	}
}
