package drcom

import (
	"strings"
	"testing"

	"github.com/love486love/epo-2026/internal/portal"
)

func TestAccount(t *testing.T) {
	cred := portal.Credential{Username: "20240001", Password: "x"}

	cases := []struct {
		name    string
		carrier string
		want    string
		wantErr bool
	}{
		{"移动", "cmcc", ",0,20240001@cmcc", false},
		{"电信", "ctcc", ",0,20240001@dx", false},
		{"联通", "cucc", ",0,20240001@lt", false},
		{"校园", "campus", ",0,20240001", false},
		{"大小写", "CMCC", ",0,20240001@cmcc", false},
		{"未知运营商", "foo", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cred.Carrier = tc.carrier
			p := LoginParams{Credential: cred}
			got, err := p.Account()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际 got=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestURLEndpoint(t *testing.T) {
	ep := portal.Endpoint{Host: "172.16.121.9", Port: 801, LoginPath: "/eportal/portal/login"}
	p := LoginParams{
		Credential: portal.Credential{Username: "20240001", Password: "secret", Carrier: "cmcc"},
		UserIP:     "10.0.0.5",
		UserMAC:    "opaque",
		AcIP:       "172.16.0.1",
	}

	u, err := p.URL(ep)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}

	wants := []string{
		"http://172.16.121.9:801/eportal/portal/login?",
		"user_account=%2C0%2C20240001%40cmcc",
		"user_password=secret",
		"wlan_user_ip=10.0.0.5",
		"wlan_user_mac=opaque",
		"wlan_ac_ip=172.16.0.1",
		"jsVersion=4.2.1",
		"login_method=1",
		"terminal_type=1",
	}
	for _, w := range wants {
		if !strings.Contains(u, w) {
			t.Errorf("URL 缺少 %q\n实际: %s", w, u)
		}
	}
}

func TestURLEmptyHost(t *testing.T) {
	p := LoginParams{Credential: portal.Credential{Username: "u", Password: "p", Carrier: "cmcc"}}
	if _, err := p.URL(portal.Endpoint{}); err == nil {
		t.Fatal("主机为空时应报错")
	}
}

func TestURLExtraOverride(t *testing.T) {
	ep := portal.Endpoint{Host: "h", Port: 801}
	p := LoginParams{
		Credential: portal.Credential{Username: "u", Password: "p", Carrier: "cmcc"},
		Extra:      map[string]string{"jsVersion": "5.0.0"},
	}
	u, err := p.URL(ep)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if !strings.Contains(u, "jsVersion=5.0.0") {
		t.Fatalf("extra_params 未生效: %s", u)
	}
}
