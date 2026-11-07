//go:build !windows

package main

// webView2Available 在非 Windows 平台恒为真，仅用于交叉编译。
func webView2Available() bool { return true }
