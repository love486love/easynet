package ruijie

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// CheckOnline 用门户权威接口复核本部是否在线（防探针抖动误判离线）。
//
// 本部网络质量差时 baidu 探针会反复 timeout/no such host，状态在
// 在线/离线间抖动；而 getOnlineUserInfo 是门户按会话/源 IP 查询的
// 在线表，比外网探针稳定得多。无法判定（网络不通/接口失败/返回
// 不明）一律返回 false，调用方维持原判定——失败无副作用。
func CheckOnline(ctx context.Context, host string, port int) bool {
	if host == "" {
		return false
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Jar:     jar,
	}
	hp := hostPort(host, port)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+hp+"/eportal/InterFace.do?method=getOnlineUserInfo",
		strings.NewReader("method=getOnlineUserInfo"))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", "http://"+hp)
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return false
	}
	var payload struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Result == "success"
}
