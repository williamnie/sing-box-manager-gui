package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xiaobei/singbox-manager/internal/api"
	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

var (
	version = "0.2.14-beta2"
	dataDir string
	port    int
)

func init() {
	// 获取默认数据目录
	homeDir, _ := os.UserHomeDir()
	defaultDataDir := filepath.Join(homeDir, ".singbox-manager")

	flag.StringVar(&dataDir, "data", defaultDataDir, "数据目录")
	flag.IntVar(&port, "port", 9090, "Web 服务端口")
}

func main() {
	flag.Parse()

	// 将 dataDir 转换为绝对路径，避免相对路径在子进程中出错
	var err error
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取绝对路径失败: %v\n", err)
		os.Exit(1)
	}

	// 获取当前可执行文件的绝对路径（用于 launchd 安装）
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取可执行文件路径失败: %v\n", err)
		os.Exit(1)
	}
	execPath, _ = filepath.EvalSymlinks(execPath)

	// 初始化日志系统
	if err := logger.InitLogManager(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志系统失败: %v\n", err)
		os.Exit(1)
	}

	// 打印启动信息
	logger.Printf("singbox-manager v%s", version)
	logger.Printf("数据目录: %s", dataDir)
	logger.Printf("Web 端口: %d", port)

	// 初始化存储
	store, err := storage.NewJSONStore(dataDir)
	if err != nil {
		logger.Printf("初始化存储失败: %v", err)
		os.Exit(1)
	}

	// 初始化进程管理器
	// sing-box 二进制文件路径固定为 dataDir/bin/sing-box
	singboxPath := filepath.Join(dataDir, "bin", "sing-box")
	configPath := filepath.Join(dataDir, "generated", "config.json")
	processManager := daemon.NewProcessManager(singboxPath, configPath, dataDir)

	// 初始化 launchd 管理器
	launchdManager, err := daemon.NewLaunchdManager()
	if err != nil {
		logger.Printf("初始化 launchd 管理器失败: %v", err)
	}

	// 初始化 systemd 管理器
	systemdManager, err := daemon.NewSystemdManager()
	if err != nil {
		logger.Printf("初始化 systemd 管理器失败: %v", err)
	}

	// 创建 API 服务器
	server := api.NewServer(store, processManager, launchdManager, systemdManager, execPath, port, version)

	// 启动定时任务调度器
	server.StartScheduler()

	// 启动服务
	addr := fmt.Sprintf(":%d", port)
	logger.Printf("启动 Web 服务: http://0.0.0.0%s", addr)

	httpServer := server.RunServer(addr)

	// 启动 HTTP 服务（非阻塞）
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Printf("启动服务失败: %v", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Printf("正在关闭服务...")

	// 优雅关闭 API 服务器（停止 worker 和调度器）
	server.Shutdown()

	// 优雅关闭 HTTP 服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Printf("服务关闭出错: %v", err)
	}

	logger.Printf("服务已关闭")
}
