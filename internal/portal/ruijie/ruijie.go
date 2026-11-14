// Package ruijie 实现本部锐捷 ePortal 登录协议。
//
// 与东区 Dr.COM 的 dr1003 JSONP 协议不同，本部（10.255.240.8）走
// POST /eportal/InterFace.do?method=login 的表单接口，参数取自
// AC 劫持页 JS 跳转（probe 从 200 正文提取）的完整 query string。
// 协议细节来自 2026-11 实测 HAR 与四轮真机抓包。
package ruijie

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/love486love/easynet/internal/portal"
)

const (
	maxBodyBytes = 8 << 10
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/155.0.0.0 Safari/537.36"
	indexPath    = "/eportal/index.jsp"
	// captchaAttempts 一轮登录里最多重取验证码的次数。
	// 实测分界清晰（同数字距离 0 / 异数字 88），识别失败几乎只因
	// 缺模板数字（1/5/7），重取 4 张覆盖概率 > 99%。
	captchaAttempts = 4
)

// ErrLogoutUnverified 表示登出接口尚未实测（与 drcom 同约定）。
var ErrLogoutUnverified = errors.New("登出接口未实测")

// Client 是锐捷 ePortal 协议的 HTTP 实现。
type Client struct {
	http *http.Client
	// CaptchaDir 是验证码模板自举库目录（成功登录后存图；空=仅用内置模板）。
	CaptchaDir string
	// retryDelay 是可重试失败后的换图间隔；New 默认 2s，测试可调小。
	retryDelay time.Duration
}

// New 返回默认客户端：整体超时 10s + Cookie Jar（门户 session 依赖 JSESSIONID）。
func New() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		http: &http.Client{
			Timeout: 10 * time.Second,
			Jar:     jar,
		},
		retryDelay: 2 * time.Second,
	}
}

func (c *Client) Name() string { return "ruijie-eportal" }

// Login 完成一次认证：
//
//  1. GET index.jsp?query 建立门户 session（JSESSIONID）；
//  2. 循环：取验证码 → OCR 识别 → POST login；
//     识别失败重取图；「超时 / 验证码错误 / Too many access」换图重试；
//     密码类错误直接返回（重试无意义）；
//  3. 成功后把「图+答案」存入模板库自举，GET success.jsp 跟随浏览器行为。
//
// 真伪复核交给上层 probe，本方法不重复探测。
func (c *Client) Login(ctx context.Context, ep portal.Endpoint, cred portal.Credential) (portal.Result, error) {
	query := ep.Extra["rawquery"]
	if query == "" {
		// 没有劫持跳转就没有 queryString（登录必被拒），属本地可恢复问题。
		return portal.Result{Code: 6, Message: "未获取门户重定向参数"},
			errors.New("缺少 index.jsp query string，无法构造本部登录请求")
	}

	hp := hostPort(ep.Host, ep.Port)
	referer := "http://" + hp + indexPath + "?" + query

	// 建立门户 session（失败不阻断：部分部署不校验 session）。
	c.get(ctx, "http://"+hp+indexPath+"?"+query, referer)

	var lastResult portal.Result
	for attempt := 0; attempt < captchaAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return portal.Result{Code: 1, Message: "网络不可达"}, err
		}

		// 每轮取新鲜验证码（门户每次刷新 rnd，旧图可能已失效）。
		png, err := c.fetchCaptcha(ctx, hp)
		if err != nil {
			// 网络卡时 validcode 常超时/返回错误页：把原始响应存下来供排障。
			c.saveFailSample("net", png)
			lastResult = portal.Result{Code: 5, Message: "获取验证码失败: " + err.Error()}
			continue
		}
		code, rerr := Recognize(png, c.CaptchaDir)
		if rerr != nil {
			// 识别不出（缺模板/非预期内容/空白图）→ 存原图、换一张，绝不猜。
			c.saveFailSample("ocr", png)
			lastResult = portal.Result{Code: 5, Message: "验证码识别失败: " + rerr.Error()}
			continue
		}

		res, userIndex, retryable := c.postLogin(ctx, hp, query, referer, cred, code)
		if res.OK {
			_ = SaveSample(c.CaptchaDir, png, code)
			if res.Message == "" {
				res.Message = "登录成功"
			}
			// 跟随浏览器完成跳转页（仅为行为一致，失败不影响认证结果）。
			if userIndex != "" {
				u := fmt.Sprintf("http://%s/eportal/success.jsp?userIndex=%s&keepaliveInterval=0",
					hp, url.QueryEscape(userIndex))
				c.get(ctx, u, referer)
			}
			return res, nil
		}
		lastResult = res
		if !retryable {
			return res, nil
		}
		// 可重试（超时/验证码错/运营商瞬时拒绝）：换新验证码再来。
		select {
		case <-ctx.Done():
			return portal.Result{Code: 1, Message: "网络不可达"}, ctx.Err()
		case <-time.After(c.retryDelay):
		}
	}
	if lastResult.Message == "" {
		lastResult.Message = "多次尝试未成功"
	}
	return lastResult, nil
}

// postLogin 提交一次登录；返回 (结果, userIndex, 该失败是否值得换验证码重试)。
func (c *Client) postLogin(ctx context.Context, hp, query, referer string, cred portal.Credential, code string) (portal.Result, string, bool) {
	form := loginForm(query, cred.Username, cred.Password, code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+hp+"/eportal/InterFace.do?method=login",
		strings.NewReader(form))
	if err != nil {
		return portal.Result{Code: 6, Message: "构造请求失败"}, "", false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", "http://"+hp)
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		// 链路未送达：上层按网络错误处理，不消耗验证码轮次。
		return portal.Result{Code: 1, Message: "网络不可达"}, "", true
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return portal.Result{
			Code:    5,
			Message: fmt.Sprintf("门户返回 HTTP %d", resp.StatusCode),
		}, "", true
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return portal.Result{Code: 5, Message: "读取门户响应失败"}, "", true
	}

	res, payload, err := ParseLoginResponse(body)
	if err != nil {
		return portal.Result{Code: 5, Message: "解析门户响应失败"}, "", true
	}
	if res.OK {
		if payload != nil {
			return res, payload.UserIndex, false
		}
		return res, "", false
	}

	// 失败分类：可重试（换新验证码） vs 硬失败。
	if payload != nil {
		msg := payload.Message
		switch {
		case strings.Contains(msg, "验证码"):
			if isCaptchaRequired(msg) {
				res.Message = "门户要求验证码（已自动识别重试）: " + msg
			}
			return res, "", true
		case strings.Contains(msg, "超时"), strings.Contains(msg, "Too many access"):
			return res, "", true
		}
	}
	return res, "", false
}

// fetchCaptcha 取一张验证码；失败时也返回已读到的原始响应（供落盘排障）。
func (c *Client) fetchCaptcha(ctx context.Context, hp string) ([]byte, error) {
	rnd := fmt.Sprintf("%.16f", float64(time.Now().UnixNano()%1e15)/1e15)
	u := "http://" + hp + "/eportal/validcode?rnd=" + rnd
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return raw, err
	}
	if resp.StatusCode != http.StatusOK {
		return raw, fmt.Errorf("验证码接口 HTTP %d（body %d 字节）", resp.StatusCode, len(raw))
	}
	if len(raw) == 0 {
		return raw, errors.New("验证码为空（服务端未就绪）")
	}
	return raw, nil
}

// saveFailSample 把失败样本落盘到 CaptchaDir（fail-<ts>-<tag>.bin）。
// loadDir 只认「4 位数字.png」，fail-* 不会混入模板库。
func (c *Client) saveFailSample(tag string, raw []byte) {
	if c.CaptchaDir == "" || len(raw) == 0 {
		return
	}
	_ = os.MkdirAll(c.CaptchaDir, 0o755)
	name := fmt.Sprintf("fail-%d-%s.bin", time.Now().UnixNano(), tag)
	_ = os.WriteFile(filepath.Join(c.CaptchaDir, name), raw, 0o600)
}

// get 发起轻量 GET，只消费状态码，任何失败都由调用方忽略。
func (c *Client) get(ctx context.Context, rawURL, referer string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
	_ = resp.Body.Close()
}

// Logout 未实测：HAR 未捕获注销请求，按 drcom 同约定不提供。
func (c *Client) Logout(ctx context.Context, ep portal.Endpoint) error {
	return ErrLogoutUnverified
}
