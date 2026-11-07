package main

// Version 是当前程序版本。发布构建时由 -ldflags "-X main.Version=<tag>" 注入，
// 本地手工构建保持下面的默认值。
var Version = "0.4.0"
