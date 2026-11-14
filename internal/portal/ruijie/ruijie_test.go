package ruijie

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/love486love/easynet/internal/portal"
)

// HAR 实测对照的 query 片段（真实抓包含完整参数，这里保留结构特征：
// key=value 用 = 和 & 连接，用于验证双重编码）。
const sampleQuery = "wlanuserip=524ff8577378bf2152bc2e49eee4b035&wlanacname=c75799cc3430d0793b0dd9e899ae5f37&ssid="

func TestLoginFormEncoding(t *testing.T) {
	const pw = "test@pass1" // 占位密码：验证 @ 的双重编码，不得使用真实凭据
	raw := loginForm(sampleQuery, "6020241922", pw, "2284")

	// 服务器端 ParseForm 只解一层，登录逻辑与 HAR 均以「预编码 + Encode」
	// 叠出两层：值先 QueryEscape 一次，Encode 再转义 % 一次。
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("表单体无法解析: %v", err)
	}
	if got, want := v.Get("password"), url.QueryEscape(pw); got != want {
		t.Errorf("password 一层解码后应为预编码值: got %q want %q", got, want)
	}
	if !strings.Contains(raw, "password=test%2540pass1") {
		t.Errorf("password 应为双重编码（对照 HAR jxau%%2540219011），实际体: %s", raw)
	}
	if got, want := v.Get("queryString"), url.QueryEscape(sampleQuery); got != want {
		t.Errorf("queryString 一层解码后应为预编码值: got %q want %q", got, want)
	}
	if !strings.Contains(raw, "queryString=wlanuserip%253D") {
		t.Errorf("queryString 的 = 应被双重编码为 %%253D，实际体: %s", raw)
	}
	if got := v.Get("userId"); got != "6020241922" {
		t.Errorf("userId 应原样: %q", got)
	}
	for _, k := range []string{"service", "operatorPwd", "operatorUserId", "validcode"} {
		if _, ok := v[k]; !ok {
			t.Errorf("缺少字段 %s", k)
		}
	}
	if v.Get("passwordEncrypt") != "false" {
		t.Errorf("passwordEncrypt 应为 false: %q", v.Get("passwordEncrypt"))
	}
	// validcode 必须携带识别值：实测空验证码会被运营商 Code 30 拒绝。
	if v.Get("validcode") != "2284" {
		t.Errorf("validcode 应为识别值 2284: %q", v.Get("validcode"))
	}
}

func TestParseLoginResponse(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantOK  bool
		wantMsg string
		wantErr bool
	}{
		{
			name:   "成功样本",
			raw:    `{"userIndex":"66613033363736653261613634666636316233303664663862336563303234305f31302e3230302e31302e35305f36303230323634383530","result":"success","message":"","forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,"validCodeUrl":""}`,
			wantOK: true,
		},
		{
			name:    "密码错误样本",
			raw:     `{"userIndex":null,"result":"fail","message":"用户不存在或者密码错误!","forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,"validCodeUrl":""}`,
			wantOK:  false,
			wantMsg: "用户不存在或者密码错误!",
		},
		{
			name:    "运营商限流样本",
			raw:     `{"userIndex":null,"result":"fail","message":"运营商用户认证失败!失败原因[[Code 30|1]: Too many access]","forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,"validCodeUrl":""}`,
			wantOK:  false,
			wantMsg: "运营商用户认证失败!失败原因[[Code 30|1]: Too many access]",
		},
		{
			name:    "验证码错误样本",
			raw:     `{"userIndex":null,"result":"fail","message":"验证码错误.","forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,"validCodeUrl":""}`,
			wantOK:  false,
			wantMsg: "验证码错误.",
		},
		{name: "空响应", raw: "", wantErr: true},
		{name: "非 JSON", raw: "<html>502</html>", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, payload, err := ParseLoginResponse([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatal("应返回解析错误")
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if res.OK != tc.wantOK {
				t.Errorf("OK=%v want %v", res.OK, tc.wantOK)
			}
			if tc.wantMsg != "" && res.Message != tc.wantMsg {
				t.Errorf("Message=%q want %q", res.Message, tc.wantMsg)
			}
			if !tc.wantOK && res.Code != 2 {
				t.Errorf("失败应为认证被拒 Code=2，实际 %d", res.Code)
			}
			if tc.wantOK && payload == nil || (tc.wantOK && payload.UserIndex == "") {
				t.Error("成功响应应带 userIndex")
			}
		})
	}
}

// mockCaptcha 返回可识别的内置验证码图（validcode-2.png = 2284）。
func mockCaptcha(w http.ResponseWriter) {
	raw, err := builtinFS.ReadFile("testdata/validcode-2.png")
	if err != nil {
		http.Error(w, "no fixture", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(raw)
}

// TestLoginFlow 用 httptest 复刻门户，核对请求形态与成功后跳转。
func TestLoginFlow(t *testing.T) {
	var gotPath, gotOrigin, gotReferer, gotCT string
	var gotForm url.Values
	var successRequested bool

	mux := http.NewServeMux()
	mux.HandleFunc("/eportal/validcode", func(w http.ResponseWriter, r *http.Request) {
		mockCaptcha(w)
	})
	mux.HandleFunc("/eportal/index.jsp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "portal page")
	})
	mux.HandleFunc("/eportal/InterFace.do", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") != "login" {
			t.Errorf("应请求 method=login，实际 %q", r.URL.RawQuery)
		}
		gotPath = r.URL.Path
		gotOrigin = r.Header.Get("Origin")
		gotReferer = r.Header.Get("Referer")
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotForm = r.Form
		_, _ = io.WriteString(w, `{"userIndex":"61616262","result":"success","message":"","forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,"validCodeUrl":""}`)
	})
	mux.HandleFunc("/eportal/success.jsp", func(w http.ResponseWriter, r *http.Request) {
		successRequested = true
		if !strings.Contains(r.URL.RawQuery, "userIndex=61616262") {
			t.Errorf("success.jsp 应携带 userIndex，实际 %q", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, "登录成功")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ep := endpointFor(t, srv.URL)
	cred := portal.Credential{Username: "6020241922", Password: "test@pass1"}

	client := New()
	res, err := client.Login(context.Background(), ep, cred)
	if err != nil {
		t.Fatalf("登录不应报错: %v", err)
	}
	if !res.OK {
		t.Fatalf("应登录成功: %+v", res)
	}
	if gotPath != "/eportal/InterFace.do" {
		t.Errorf("路径 %q", gotPath)
	}
	if !strings.HasPrefix(gotReferer, "http://"+ep.Host+":") || !strings.Contains(gotReferer, "/eportal/index.jsp?"+sampleQuery) {
		t.Errorf("Referer 应为完整 index.jsp?query，实际 %q", gotReferer)
	}
	if !strings.HasPrefix(gotOrigin, "http://"+ep.Host+":") {
		t.Errorf("Origin 应指向门户，实际 %q", gotOrigin)
	}
	if !strings.HasPrefix(gotCT, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type %q", gotCT)
	}
	if gotForm.Get("userId") != cred.Username {
		t.Errorf("userId %q", gotForm.Get("userId"))
	}
	// 识别出的验证码必须随表单提交（2284 来自 mock 验证码图）。
	if gotForm.Get("validcode") != "2284" {
		t.Errorf("validcode 应为 OCR 结果 2284，实际 %q", gotForm.Get("validcode"))
	}
	if !successRequested {
		t.Error("成功后应请求 success.jsp")
	}
}

// TestLoginCaptchaRequired 断言「验证码错误」自动换图重试，
// 用尽轮次后如实上报（带已自动识别重试的说明）。
func TestLoginCaptchaRequired(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/eportal/validcode", func(w http.ResponseWriter, r *http.Request) {
		mockCaptcha(w)
	})
	mux.HandleFunc("/eportal/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "InterFace.do") {
			_, _ = io.WriteString(w, `{"userIndex":null,"result":"fail","message":"验证码错误.","forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,"validCodeUrl":"/eportal/validcode?rnd=0.5"}`)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New()
	client.retryDelay = 0 // 测试不等待
	res, err := client.Login(context.Background(), endpointFor(t, srv.URL),
		portal.Credential{Username: "6020241922", Password: "test@pass1"})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res.OK {
		t.Fatal("应判定失败")
	}
	if !strings.Contains(res.Message, "验证码") || !strings.Contains(res.Message, "已自动识别重试") {
		t.Errorf("验证码失败应带自动识别重试说明，实际 %q", res.Message)
	}
}

// TestLoginMissingQuery 模拟探针没拿到 AC 重定向：本地可恢复错误。
func TestLoginMissingQuery(t *testing.T) {
	ep := portal.Endpoint{Host: "10.255.240.8", Port: 80, Extra: map[string]string{}}
	res, err := New().Login(context.Background(), ep, portal.Credential{Username: "u", Password: "p"})
	if err == nil {
		t.Fatal("缺少 query 应返回错误")
	}
	if res.Code != 6 {
		t.Errorf("Code 应为 6（本地参数问题），实际 %d", res.Code)
	}
	if errors.Is(err, context.Canceled) {
		t.Error("不应是 context 错误")
	}
}

// endpointFor 把 httptest 地址转换为门户 Endpoint（复刻本部重定向解析结果）。
func endpointFor(t *testing.T, rawURL string) portal.Endpoint {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("解析测试地址: %v", err)
	}
	port := 80
	if p := u.Port(); p != "" {
		if n, convErr := strconv.Atoi(p); convErr == nil {
			port = n
		}
	}
	return portal.Endpoint{
		Host:  u.Hostname(),
		Port:  port,
		Extra: map[string]string{"rawquery": sampleQuery, "indexpath": "/eportal/index.jsp"},
	}
}
