package drcom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/love486love/easynet/internal/portal"
)

const (
	maxBodyBytes = 8 << 10
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/155.0.0.0 Safari/537.36"
)

// ErrLogoutUnverified 表示登出接口尚未实测。
// 调用方应据此隐藏该入口，而不是向用户暴露一个未验证的功能。
var ErrLogoutUnverified = errors.New("登出接口未实测")

// Client 是 ePortal 协议的 HTTP 实现。
type Client struct {
	http     *http.Client
	suffixes map[string]string
}

// New 返回默认客户端（使用内置运营商后缀表，整体超时 10s）。
func New() *Client {
	return &Client{http: &http.Client{Timeout: 10 * time.Second}}
}

// NewWithSuffixes 使用 profile 提供的运营商后缀表，覆盖内置默认值。
func NewWithSuffixes(suffixes map[string]string) *Client {
	return &Client{
		http:     &http.Client{Timeout: 10 * time.Second},
		suffixes: suffixes,
	}
}

func (c *Client) Name() string { return "drcom-eportal" }

// Login 完成一次认证。所有网关参数从 ep.Extra 获取，
// 这些值由前置的探针重定向解析得到；缺失时不阻断，仅由调用方告警。
func (c *Client) Login(ctx context.Context, ep portal.Endpoint, cred portal.Credential) (portal.Result, error) {
	params := LoginParams{
		Credential: cred,
		UserIP:     ep.Extra["userip"],
		UserMAC:    ep.Extra["usermac"],
		AcIP:       ep.Extra["wlanacip"],
		Suffixes:   c.suffixes,
		Extra:      ep.Extra,
	}

	loginURL, err := params.URL(ep)
	if err != nil {
		return portal.Result{Code: 6, Message: err.Error()}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return portal.Result{Code: 6, Message: "构造请求失败"}, err
	}
	req.Header.Set("Referer", fmt.Sprintf("http://%s/", ep.Host))
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return portal.Result{Code: 1, Message: "网络不可达"}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return portal.Result{
			Code:    5,
			Message: fmt.Sprintf("门户返回 HTTP %d", resp.StatusCode),
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return portal.Result{Code: 5, Message: "读取门户响应失败"}, err
	}
	return ParseLoginResponse(body)
}

// Logout 未实现：M0 与 HAR 均未捕获过注销请求，按文档约定暂不提供。
func (c *Client) Logout(ctx context.Context, ep portal.Endpoint) error {
	return ErrLogoutUnverified
}
