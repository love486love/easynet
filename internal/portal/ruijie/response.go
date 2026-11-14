package ruijie

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/love486love/easynet/internal/portal"
)

// loginPayload 是 InterFace.do?method=login 的响应体（HAR 实测样本）：
//
//	{"userIndex":null,"result":"fail","message":"用户不存在或者密码错误!",
//	 "forwordurl":null,"keepaliveInterval":0,"casFailErrString":null,
//	 "validCodeUrl":"/eportal/validcode?rnd=0.4622783461898138"}
//
// 成功样本：userIndex 为 hex 串、result 为 "success"。
type loginPayload struct {
	UserIndex         string `json:"userIndex"`
	Result            string `json:"result"`
	Message           string `json:"message"`
	ForwordURL        string `json:"forwordurl"`
	KeepaliveInterval int    `json:"keepaliveInterval"`
	CasFailErrString  string `json:"casFailErrString"`
	ValidCodeUrl      string `json:"validCodeUrl"`
}

// ParseLoginResponse 解析锐捷门户登录响应。
//
// 成功判定：result == "success"（返回 userIndex，后续 success.jsp 需要）。
// 失败一律按认证被拒（Code 2）处理，message 原文保留给日志；
// 验证码相关失败由调用方按 message 关键词识别。
func ParseLoginResponse(raw []byte) (portal.Result, *loginPayload, error) {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return portal.Result{}, nil, fmt.Errorf("响应为空")
	}
	var p loginPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return portal.Result{}, nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if p.Result == "success" || strings.Contains(p.Message, "已经在线") {
		return portal.Result{OK: true, Message: "登录成功"}, &p, nil
	}
	res := portal.Result{Code: 2, Message: p.Message}
	if res.Message == "" {
		res.Message = "门户拒绝登录"
	}
	return res, &p, nil
}
