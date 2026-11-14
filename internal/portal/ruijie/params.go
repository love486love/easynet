package ruijie

import (
	"net/url"
	"strconv"
	"strings"
)

// loginForm 构造 InterFace.do?method=login 的表单体。
//
// 编码规则（HAR 实测）：每个值先 url.QueryEscape 一次，再交给
// url.Values.Encode 整体编码一次——含特殊字符的值最终是双重编码：
//
//	密码 jxau@219011          → password=jxau%2540219011
//	query wlanuserip=...&...  → queryString=wlanuserip%253D...%2526...
//
// 无特殊字符的值（学号、空值）两层与一层无差别，与 HAR 一致。
// validcode 必须是识别出的 4 位验证码：实测空验证码会走到运营商环节
// 被 Code 30 (Too many access) 拒绝，正确验证码才能通过。
func loginForm(query, user, pass, code string) string {
	v := url.Values{}
	v.Set("userId", url.QueryEscape(user))
	v.Set("password", url.QueryEscape(pass))
	v.Set("service", "")
	v.Set("queryString", url.QueryEscape(query))
	v.Set("operatorPwd", "")
	v.Set("operatorUserId", "")
	v.Set("validcode", url.QueryEscape(code))
	v.Set("passwordEncrypt", "false")
	return v.Encode()
}

// hostPort 返回 Origin/Referer 用的主机串；80 端口省略（与浏览器一致）。
func hostPort(host string, port int) string {
	if host == "" {
		return ""
	}
	if port > 0 && port != 80 {
		return host + ":" + strconv.Itoa(port)
	}
	return host
}

// isCaptchaRequired 判断失败 message 是否为验证码类拒绝。
func isCaptchaRequired(msg string) bool {
	return strings.Contains(msg, "验证码")
}
