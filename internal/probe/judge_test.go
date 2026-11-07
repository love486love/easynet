package probe

import (
	"testing"

	"github.com/love486love/epo-2026/internal/portal"
)

const (
	schoolProbe = "http://www.jxau.edu.cn/?epo=1"
	baiduProbe  = "http://www.baidu.com/?epo=1"
	schoolExp   = "jxau.edu.cn"
)

var schoolHosts = []string{"www.jxau.edu.cn", "jxau.edu.cn"}

func TestJudge(t *testing.T) {
	cases := []struct {
		name    string
		probe   string
		allowed []string
		expect  string
		ev      Evidence
		want    portal.State
	}{
		{
			name:    "M0实测：离线302到门户",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev: Evidence{
				StatusCode: 302,
				Location:   "http://172.16.121.9/a79.htm?userip=10.0.0.5&wlanacip=172.16.0.1",
				Server:     "NetEngine Server 1.0",
			},
			want: portal.StateOffline,
		},
		{
			name:    "在线：302跳HTTPS同主机",
			probe:   baiduProbe,
			allowed: []string{"www.baidu.com"},
			expect:  "baidu.com",
			ev: Evidence{
				StatusCode: 302,
				Location:   "https://www.baidu.com/?epo=1",
			},
			want: portal.StateOnline,
		},
		{
			name:    "在线：302去www仍算同站",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev: Evidence{
				StatusCode: 301,
				Location:   "https://jxau.edu.cn/",
			},
			want: portal.StateOnline,
		},
		{
			name:    "在线：2xx命中期望串（大小写不敏感）",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev: Evidence{
				StatusCode: 200,
				Body:       []byte("<title>江西农业大学</title><link href=\"HTTP://WWW.JXAU.EDU.CN/x\">"),
			},
			want: portal.StateOnline,
		},
		{
			name:    "在线：206分段响应命中期望串",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev: Evidence{
				StatusCode: 206,
				Body:       []byte("<head><meta url=jxau.edu.cn></head>"),
			},
			want: portal.StateOnline,
		},
		{
			name:    "未认证：200但未命中期望串（透明劫持）",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev: Evidence{
				StatusCode: 200,
				Body:       []byte("<html>请先登录</html>"),
			},
			want: portal.StateOffline,
		},
		{
			name:    "未认证：200空正文但配置了expect",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev:      Evidence{StatusCode: 200},
			want:    portal.StateOffline,
		},
		{
			name:    "未配置expect：退回特征排除，普通页算在线",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  "",
			ev:      Evidence{StatusCode: 200, Body: []byte("<html>hello</html>")},
			want:    portal.StateOnline,
		},
		{
			name:    "未配置expect：命中拦截特征算离线",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  "",
			ev: Evidence{
				StatusCode: 200,
				Body:       []byte(`<WISPAccessGatewayParam><MessageType>110</MessageType>`),
			},
			want: portal.StateOffline,
		},
		{
			name:    "未知：500不计为证据",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev:      Evidence{StatusCode: 500},
			want:    portal.StateUnknown,
		},
		{
			name:    "未知分支保守处理：404按未认证",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev:      Evidence{StatusCode: 404},
			want:    portal.StateOffline,
		},
		{
			name:    "3xx缺Location按未认证",
			probe:   schoolProbe,
			allowed: schoolHosts,
			expect:  schoolExp,
			ev:      Evidence{StatusCode: 302},
			want:    portal.StateOffline,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Judge(tc.probe, tc.allowed, tc.expect, tc.ev)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSameSite(t *testing.T) {
	if !sameSite("www.jxau.edu.cn", []string{"jxau.edu.cn"}) {
		t.Fatal("子域应视为同站")
	}
	if sameSite("evil-jxau.edu.cn", []string{"jxau.edu.cn"}) {
		t.Fatal("后缀伪装不应视为同站")
	}
	if sameSite("", []string{"jxau.edu.cn"}) {
		t.Fatal("空 host 不应通过")
	}
}
