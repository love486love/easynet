//go:build !windows

package main

// runTray 在非 Windows 平台没有实现；本项目仅支持 Windows，保留空实现以便交叉编译。
func runTray() {}
