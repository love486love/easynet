package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/love486love/epo-2026/internal/app"
	"github.com/love486love/epo-2026/internal/autostart"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/creds"
)

// cmdSaveCred 把 JSON 凭据用 DPAPI 加密后写入本地目录。
func cmdSaveCred(args []string) error {
	fs := flag.NewFlagSet("save-cred", flag.ExitOnError)
	credPath := fs.String("cred", "", "凭据文件（JSON，含 username/password/carrier）")
	_ = fs.Parse(args)

	if *credPath == "" {
		return fmt.Errorf("必须提供 -cred 凭据文件")
	}
	cred, err := readCredential(*credPath)
	if err != nil {
		return err
	}
	dir, err := config.UserDir()
	if err != nil {
		return err
	}
	secret := creds.Secret{Username: cred.Username, Password: cred.Password, Carrier: cred.Carrier}
	if err := creds.Save(dir, secret); err != nil {
		return err
	}
	fmt.Println("凭据已加密保存:", creds.Path(dir))
	return nil
}

// cmdDaemon 启动常驻守护，直到 Ctrl+C 或 GUI 请求停止。
func cmdDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	_ = fs.Parse(args)
	return app.RunDaemon(*profilePath)
}

// cmdAutostart 管理开机自启（HKCU\Run，无需管理员权限）。
func cmdAutostart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: epo autostart install|remove|status")
	}

	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		target := filepath.Join(filepath.Dir(exe), "epo-daemon.exe")
		if _, statErr := os.Stat(target); statErr != nil {
			return fmt.Errorf("未找到 %s，请先构建 epo-daemon.exe（windowsgui 子系统）", target)
		}
		if err := autostart.Enable(target); err != nil {
			return err
		}
		fmt.Println("已启用开机自启:", target)
	case "remove":
		if err := autostart.Disable(); err != nil {
			return err
		}
		fmt.Println("已移除开机自启")
	case "status":
		v, err := autostart.Value()
		if err != nil {
			return err
		}
		if v == "" {
			fmt.Println("开机自启: 未启用")
		} else {
			fmt.Println("开机自启: 已启用 ->", v)
		}
	default:
		return fmt.Errorf("未知子命令 %q", args[0])
	}
	return nil
}
