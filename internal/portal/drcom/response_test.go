package drcom

import (
	"testing"
)

func TestParseLoginSuccess(t *testing.T) {
	raw := []byte(`dr1003({"result":1,"msg":"Portal协议认证成功！"});`)
	res, err := ParseLoginResponse(raw)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if !res.OK {
		t.Fatal("result=1 应判为成功")
	}
	if res.Message == "" {
		t.Fatal("应保留服务端 msg")
	}
}

func TestParseLoginFailure(t *testing.T) {
	raw := []byte(`dr1003({"result":0,"msg":"账号不存在","ret_code":1});`)
	res, err := ParseLoginResponse(raw)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if res.OK {
		t.Fatal("result=0 应判为失败")
	}
	if res.Code != 2 {
		t.Fatalf("失败应归 Code 2，实际 %d", res.Code)
	}
	if res.RetCode != 1 {
		t.Fatalf("ret_code 应保留，实际 %d", res.RetCode)
	}
	if res.Message != "账号不存在" {
		t.Fatalf("应透传 msg，实际 %q", res.Message)
	}
}

func TestParseAlreadyOnline(t *testing.T) {
	raw := []byte(`dr1003({"result":0,"msg":"您已经在线，无需重复登录"});`)
	res, err := ParseLoginResponse(raw)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if !res.OK {
		t.Fatal("“已经在线”应视为成功，否则守护进程会反复重试")
	}
}

func TestParsePlainJSON(t *testing.T) {
	raw := []byte(`{"result":1,"msg":"ok"}`)
	res, err := ParseLoginResponse(raw)
	if err != nil {
		t.Fatalf("无 JSONP 外壳也应能解析: %v", err)
	}
	if !res.OK {
		t.Fatal("应判为成功")
	}
}

func TestParseMalformed(t *testing.T) {
	if _, err := ParseLoginResponse([]byte(`not json`)); err == nil {
		t.Fatal("非法内容应报错")
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := ParseLoginResponse(nil); err == nil {
		t.Fatal("空响应应报错")
	}
}
