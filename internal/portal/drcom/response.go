package drcom

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/love486love/epo-2026/internal/portal"
)

// JSONP 外壳：dr1003({...}); 回调名不固定，用正则剥离。
var jsonpWrapper = regexp.MustCompile(`^\s*[\w$.]+\s*\(([\s\S]*)\)\s*;?\s*$`)

type loginPayload struct {
	Result  int    `json:"result"`
	Msg     string `json:"msg"`
	RetCode int    `json:"ret_code"`
}

// ParseLoginResponse 解析登录接口响应。
//
// 成功判定：result==1，或服务端明确表示"已经在线"。
// 后者是同一网关在重复登录时的正常返回，按失败处理会导致守护进程
// 误以为没登上去，从而反复重试。
func ParseLoginResponse(raw []byte) (portal.Result, error) {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return portal.Result{}, fmt.Errorf("响应为空")
	}
	if m := jsonpWrapper.FindStringSubmatch(body); m != nil {
		body = strings.TrimSpace(m[1])
	}

	var p loginPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return portal.Result{}, fmt.Errorf("解析响应失败: %w", err)
	}

	res := portal.Result{
		Message: p.Msg,
		RetCode: p.RetCode,
	}
	res.OK = p.Result == 1 || strings.Contains(p.Msg, "已经在线")
	if !res.OK {
		res.Code = 2 // 认证被拒
	}
	return res, nil
}
