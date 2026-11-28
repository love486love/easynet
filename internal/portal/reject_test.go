package portal

import "testing"

func TestClassifyReject(t *testing.T) {
	cases := []struct {
		name string
		res  Result
		want RejectKind
	}{
		{"成功不归类", Result{OK: true, Message: "登录成功"}, RejectUnknown},
		{"空文案", Result{Code: 2}, RejectUnknown},
		{"时间策略", Result{Code: 2, Message: "当前不在允许上网的时间段"}, RejectTimePolicy},
		{"时间策略-时段", Result{Code: 2, Message: "非上网时间，禁止登录"}, RejectTimePolicy},
		{"密码错误", Result{Code: 2, Message: "用户不存在或者密码错误!"}, RejectBadCredential},
		{"密码不正确", Result{Code: 2, Message: "密码不正确"}, RejectBadCredential},
		{"账号不存在", Result{Code: 2, Message: "账号不存在"}, RejectBadCredential},
		{"已锁定", Result{Code: 2, Message: "密码错误次数过多，账号已锁定"}, RejectLocked},
		{"欠费", Result{Code: 2, Message: "账号欠费停机"}, RejectLocked},
		{"并发", Result{Code: 2, Message: "该账号同时在线数已达上限"}, RejectConcurrency},
		{"未知原因", Result{Code: 2, Message: "认证失败"}, RejectUnknown},
	}
	for _, tc := range cases {
		if got := ClassifyReject(tc.res); got != tc.want {
			t.Errorf("%s: ClassifyReject=%v want %v", tc.name, got, tc.want)
		}
	}
}
