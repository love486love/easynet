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
// 成功判定只认 result==1，其余一律按拒绝处理并透传服务端 msg。
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
		OK:      p.Result == 1,
		Message: p.Msg,
		RetCode: p.RetCode,
	}
	if !res.OK {
		res.Code = 2 // 认证被拒
	}
	return res, nil
}
