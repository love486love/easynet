//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// WebView2 Runtime 的 EdgeUpdate 客户端键（GUID 固定）。
const webview2ClientKey = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// webView2Available 检查系统是否安装了 WebView2 Runtime（机器级或用户级）。
func webView2Available() bool {
	roots := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\` + webview2ClientKey},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\` + webview2ClientKey},
		{registry.CURRENT_USER, `SOFTWARE\` + webview2ClientKey},
	}
	for _, r := range roots {
		if k, err := registry.OpenKey(r.root, r.path, registry.QUERY_VALUE); err == nil {
			_ = k.Close()
			return true
		}
	}
	return false
}
