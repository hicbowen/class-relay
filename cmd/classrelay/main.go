package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"class-relay/internal/network"
	"class-relay/internal/server"
	"class-relay/internal/storage"
)

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Run()
}

func run() error {
	adminPort := flag.Int("teacher-port", 17888, "教师管理端口")
	studentPort := flag.Int("student-port", 17887, "学生访问端口")
	dataDir := flag.String("data-dir", "", "SQLite 数据目录（云端默认 ./data，局域网默认系统用户配置目录）")
	noBrowser := flag.Bool("no-browser", false, "不自动打开浏览器")
	cloudURL := flag.String("cloud-url", "", "启用云端模式并设置公网 HTTPS 地址")
	teacherUser := flag.String("cloud-user", "teacher", "云端教师账号")
	passwordFile := flag.String("cloud-password-file", "teacher-password", "云端教师密码文件路径（相对于当前目录）")
	flag.Parse()
	if *adminPort < 1 || *adminPort > 65535 || *studentPort < 1 || *studentPort > 65535 {
		return errors.New("端口必须在 1 到 65535 之间")
	}
	if *dataDir == "" && *cloudURL != "" {
		*dataDir = "data"
	}
	if *dataDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*dataDir = filepath.Join(dir, "ClassRelay")
	}
	host := net.JoinHostPort("127.0.0.1", strconv.Itoa(*adminPort))
	listener, err := network.ListenIPv4(host)
	if err != nil {
		return fmt.Errorf("无法监听教师端 %s，检查端口是否已被占用：%w", host, err)
	}
	defer listener.Close()
	db, err := storage.Open(*dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	var app *server.App
	var handler http.Handler
	if *cloudURL != "" {
		data, err := os.ReadFile(*passwordFile)
		if err != nil {
			return fmt.Errorf("读取教师密码文件：%w", err)
		}
		password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		clear(data)
		app, err = server.NewCloud(db, *cloudURL, *teacherUser, password)
		if err != nil {
			return err
		}
		handler = app.Cloud()
	} else {
		app, err = server.New(db, strconv.Itoa(*studentPort))
		if err != nil {
			return err
		}
		handler = app.Teacher(host)
	}
	defer app.Close()
	admin := network.HTTP(handler)
	admin.BaseContext = app.BaseContext
	failures := make(chan error, 1)
	go func() { failures <- admin.Serve(listener) }()

	if *cloudURL != "" {
		origin := strings.TrimSuffix(*cloudURL, "/")
		fmt.Println("ClassRelay 云端教师端", origin+"/teacher")
		fmt.Println("ClassRelay 固定学生入口", origin+"/student")
		fmt.Println("HTTPS 反向代理上游", host)
	} else {
		entry := "http://" + host + "/teacher"
		fmt.Println("ClassRelay 教师端", entry)
		fmt.Println("本次启动的教师登录链接（仅在教师电脑使用）", entry+"#key="+app.LoginKey())
		if !*noBrowser {
			go func() {
				if err := openBrowser(entry + "#key=" + app.LoginKey()); err != nil {
					log.Printf("无法自动打开浏览器，请使用终端中的登录链接：%v", err)
				}
			}()
		}
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case <-signals:
		app.Close()
		network.Shutdown(admin)
		return nil
	case err := <-failures:
		return err
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
