package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/xiaobei/singbox-manager/internal/api"
	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

var (
	Version                                   = "0.2.13"
	BuildTime                                 = "unknown"
	GitCommit                                 = "unknown"
	dataDir                                   string
	port                                      int
	listenHost, tlsCert, tlsKey, allowNetwork string
)

func init() {
	homeDir, _ := os.UserHomeDir()
	flag.StringVar(&dataDir, "data", filepath.Join(homeDir, ".singbox-manager"), "数据目录")
	flag.IntVar(&port, "port", 9090, "Web 服务端口")
	flag.StringVar(&listenHost, "listen", "127.0.0.1", "Web 监听 IP（非回环地址必须配置 TLS）")
	flag.StringVar(&tlsCert, "tls-cert", "", "管理服务 TLS 证书路径")
	flag.StringVar(&tlsKey, "tls-key", "", "管理服务 TLS 私钥路径")
	flag.StringVar(&allowNetwork, "allow-network", "", "允许访问管理服务的来源 CIDR，逗号分隔；空值不附加来源限制")
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--internal-log-relay" {
		if err := daemon.RunLogRelay(os.Args[2], os.Args[3]); err != nil {
			os.Exit(1)
		}
		return
	}
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动管理器失败: %v\n", err)
		os.Exit(1)
	}
}

func validateListen(host string, port int, cert, key, networks string) (string, []*net.IPNet, error) {
	ip := net.ParseIP(host)
	if ip == nil {
		return "", nil, fmt.Errorf("-listen 必须是明确的 IPv4 或 IPv6 地址")
	}
	if port < 1 || port > 65535 {
		return "", nil, fmt.Errorf("Web 端口必须在 1 到 65535 之间")
	}
	if (cert == "") != (key == "") {
		return "", nil, fmt.Errorf("-tls-cert 和 -tls-key 必须同时配置")
	}
	if !ip.IsLoopback() && cert == "" {
		return "", nil, fmt.Errorf("非本机监听必须配置 -tls-cert 和 -tls-key")
	}
	var allowed []*net.IPNet
	if networks != "" {
		for _, entry := range strings.Split(networks, ",") {
			_, network, err := net.ParseCIDR(strings.TrimSpace(entry))
			if err != nil {
				return "", nil, fmt.Errorf("-allow-network 含无效 CIDR")
			}
			allowed = append(allowed, network)
		}
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), allowed, nil
}

func run() error {
	addr, allowed, err := validateListen(listenHost, port, tlsCert, tlsKey, allowNetwork)
	if err != nil {
		return err
	}
	if tlsCert != "" {
		if _, err := tls.LoadX509KeyPair(tlsCert, tlsKey); err != nil {
			return fmt.Errorf("无法加载管理服务 TLS 证书或私钥: %w", err)
		}
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	lock, err := daemon.AcquireInstanceLock(dataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = resolved
	}
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	if err := logger.InitLogManager(dataDir); err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	logger.Printf("singbox-manager v%s", Version)
	logger.Printf("数据目录: %s", dataDir)
	store, err := storage.NewJSONStore(dataDir)
	if err != nil {
		return fmt.Errorf("初始化存储失败: %w", err)
	}
	processManager := daemon.NewProcessManager(filepath.Join(dataDir, "bin", "sing-box"), filepath.Join(dataDir, "generated", "config.json"), dataDir)
	launchdManager, err := daemon.NewLaunchdManager()
	if err != nil {
		logger.Printf("launchd 管理不可用: %v", err)
	}
	systemdManager, err := daemon.NewSystemdManager()
	if err != nil {
		logger.Printf("systemd 管理不可用: %v", err)
	}
	server := api.NewServer(store, processManager, launchdManager, systemdManager, execPath, port, Version)
	if !server.AuthReady() {
		return fmt.Errorf("认证初始化失败，管理服务未启动；请检查数据目录中的认证文件权限和格式")
	}
	server.SetAllowedNetworks(allowed)
	if _, err := os.Stat(filepath.Join(dataDir, "setup-token")); err == nil {
		logger.Printf("首次登录初始化令牌文件: %s", filepath.Join(dataDir, "setup-token"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server.StartScheduler()
	defer server.StopScheduler()
	scheme := "http"
	if tlsCert != "" {
		scheme = "https"
	}
	logger.Printf("管理服务地址: %s://%s", scheme, addr)
	serveErrors := make(chan error, 1)
	go func() {
		if tlsCert != "" {
			serveErrors <- server.RunTLS(addr, tlsCert, tlsKey)
		} else {
			serveErrors <- server.Run(addr)
		}
	}()
	select {
	case err := <-serveErrors:
		return err
	case <-ctx.Done():
		// 管理器退出不终止已记录身份的 sing-box，下次启动按身份恢复。
		logger.Printf("管理器退出，保留受管 sing-box 运行状态")
		return nil
	}
}
