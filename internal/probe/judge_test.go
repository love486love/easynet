package probe

import (
	"testing"

	"github.com/love486love/epo-2026/internal/portal"
)

const (
	schoolProbe = "http://www.jxau.edu.cn/?epo=1"
	baiduProbe  = "http://www.baidu.com/?epo=1"
)

var schoolHosts = []string{"www.jxau.edu.cn", "jxau.edu.cn"}

func TestJudge(t *testing.T) {
	cases := []struct {
		name    string
		probe   string
		allowed []string
		ev      Evidence
		want    portal.State
	}{
		{
			name:    "M0实测：离线302到门户",
			probe:   schoolProbe,
			allowed: schoolHosts,
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
			ev: Evidence{
				StatusCode: 301,
				Location:   "https://jxau.edu.cn/",
			},
			want: portal.StateOnline,
		},
		{
			name:    "在线：200无正文",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev:      Evidence{StatusCode: 200},
			want:    portal.StateOnline,
		},
		{
			name:    "在线：206分段响应",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev: Evidence{
				StatusCode: 206,
				Body:       []byte("<!DOCTYPE html><html><head>"),
			},
			want: portal.StateOnline,
		},
		{
			name:    "未认证：200直出登录页",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev: Evidence{
				StatusCode: 200,
				Body:       []byte(`<WISPAccessGatewayParam><MessageType>110</MessageType>`),
			},
			want: portal.StateOffline,
		},
		{
			name:    "未认证：200但含密码输入框",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev: Evidence{
				StatusCode: 200,
				Body:       []byte(`<form><input type="password" name="upass"></form>`),
			},
			want: portal.StateOffline,
		},
		{
			name:    "未知：500不计为证据",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev:      Evidence{StatusCode: 500},
			want:    portal.StateUnknown,
		},
		{
			name:    "未知分支保守处理：404按未认证",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev:      Evidence{StatusCode: 404},
			want:    portal.StateOffline,
		},
		{
			name:    "3xx缺Location按未认证",
			probe:   schoolProbe,
			allowed: schoolHosts,
			ev:      Evidence{StatusCode: 302},
			want:    portal.StateOffline,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Judge(tc.probe, tc.allowed, tc.ev)
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
