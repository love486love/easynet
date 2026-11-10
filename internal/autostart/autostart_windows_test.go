//go:build windows

package autostart

import "testing"

func TestParseRunValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"C:\easynet\easynet.exe" --daemon`, `C:\easynet\easynet.exe`},
		{`"C:\old dir\easynet.exe"`, `C:\old dir\easynet.exe`},
		{`C:\easynet\easynet.exe --daemon`, `C:\easynet\easynet.exe`},
		{`C:\easynet\easynet.exe`, `C:\easynet\easynet.exe`},
		{"  \"D:\\ops测试\\easynet.exe\" --daemon", `D:\ops测试\easynet.exe`},
		{"", ""},
	}
	for _, c := range cases {
		if got := parseRunValue(c.in); got != c.want {
			t.Errorf("parseRunValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
