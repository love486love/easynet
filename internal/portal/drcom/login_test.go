package drcom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/love486love/easynet/internal/portal"
)

// newTestClient 启动一个模拟门户的本地服务，返回注入好 http.Client 的实例。
// 这样 Login 的完整链路（构造参数 → 发请求 → 解析响应）都能被覆盖。
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, portal.Endpoint) {
	t.Helper()

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("解析测试服务地址失败: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("解析测试服务端口失败: %v", err)
	}

	ep := portal.Endpoint{
		Host:      u.Hostname(),
		Port:      port,
		LoginPath: "/eportal/portal/login",
		Extra: map[string]string{
			"userip":   "10.1.2.3",
			"usermac":  "aa:bb:cc:dd:ee:ff",
			"wlanacip": "172.16.0.1",
		},
	}
	return &Client{http: ts.Client()}, ep
}

func testCredential() portal.Credential {
	return portal.Credential{Username: "2023001", Password: "secret", Carrier: "cmcc"}
}

func TestLoginSuccessOverHTTP(t *testing.T) {
	var gotAccount, gotPassword, gotCallback string
	c, ep := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotAccount = q.Get("user_account")
		gotPassword = q.Get("user_password")
		gotCallback = q.Get("callback")
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(`dr1003({"result":1,"msg":"Portal协议认证成功！"});`))
	})

	res, err := c.Login(context.Background(), ep, testCredential())
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if !res.OK {
		t.Fatalf("应判为成功，实际 code=%d msg=%q", res.Code, res.Message)
	}
	if gotAccount != ",0,2023001@cmcc" {
		t.Fatalf("user_account 构造错误: %q", gotAccount)
	}
	if gotPassword != "secret" {
		t.Fatalf("密码应原样传递，实际 %q", gotPassword)
	}
	if gotCallback != "dr1003" {
		t.Fatalf("callback 应为 dr1003，实际 %q", gotCallback)
	}
}

func TestLoginMalformedJSONP(t *testing.T) {
	c, ep := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`dr1003({"result":1,`))
	})

	if _, err := c.Login(context.Background(), ep, testCredential()); err == nil {
		t.Fatal("畸形 JSONP 应返回错误，而不是被当成成功")
	}
}

func TestLoginHTTPError(t *testing.T) {
	c, ep := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	res, err := c.Login(context.Background(), ep, testCredential())
	if err != nil {
		t.Fatalf("HTTP 500 不应返回 Go 错误: %v", err)
	}
	if res.OK {
		t.Fatal("HTTP 500 不应判为成功")
	}
	if res.Code != 5 {
		t.Fatalf("HTTP 错误应归 Code 5，实际 %d", res.Code)
	}
	if !strings.Contains(res.Message, "500") {
		t.Fatalf("提示应包含状态码，实际 %q", res.Message)
	}
}

func TestLoginFollowsRedirect(t *testing.T) {
	c, ep := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stage") != "final" {
			http.Redirect(w, r, "/eportal/portal/login?stage=final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`dr1003({"result":1,"msg":"ok"});`))
	})

	res, err := c.Login(context.Background(), ep, testCredential())
	if err != nil {
		t.Fatalf("跟随重定向后应能成功: %v", err)
	}
	if !res.OK {
		t.Fatal("重定向到正常响应后应判为成功")
	}
}

func TestLoginTruncatesOversizeBody(t *testing.T) {
	c, ep := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// 超过 maxBodyBytes 的响应会被截断，截断后的 JSON 不完整，必须报错。
		body := `dr1003({"result":1,"msg":"` + strings.Repeat("a", 20<<10) + `"});`
		_, _ = w.Write([]byte(body))
	})

	if _, err := c.Login(context.Background(), ep, testCredential()); err == nil {
		t.Fatal("超长响应截断后应返回错误")
	}
}
