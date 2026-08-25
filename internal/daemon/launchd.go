package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"text/template"
	"time"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.SbmPath}}</string>
        <string>-data</string>
        <string>{{.DataDir}}</string>
        <string>-port</string>
        <string>{{.Port}}</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>{{.HomeDir}}</string>
    </dict>
    <key>RunAtLoad</key>
    <{{if .RunAtLoad}}true{{else}}false{{end}}/>
    <key>KeepAlive</key>
    <{{if .KeepAlive}}true{{else}}false{{end}}/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}/sbm.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}/sbm.error.log</string>
    <key>WorkingDirectory</key>
    <string>{{.WorkingDir}}</string>
</dict>
</plist>`

// LaunchdConfig launchd 配置
type LaunchdConfig struct {
	Label      string
	SbmPath    string // sbm 可执行文件路径
	DataDir    string // 数据目录
	Port       string // Web 端口
	LogPath    string
	WorkingDir string
	HomeDir    string // 用户主目录，用于设置 HOME 环境变量
	RunAtLoad  bool
	KeepAlive  bool
}

// LaunchdManager launchd 管理器
type LaunchdManager struct {
	label     string
	plistPath string
}

// NewLaunchdManager 创建 launchd 管理器
func NewLaunchdManager() (*LaunchdManager, error) {
	// 只在 macOS 上支持 launchd
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("launchd 仅在 macOS 上支持")
	}

	homeDir, err := getUserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}

	label := "com.singbox.manager"
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", label+".plist")

	return &LaunchdManager{
		label:     label,
		plistPath: plistPath,
	}, nil
}

// getUserHomeDir 获取用户主目录，支持多种方式
func getUserHomeDir() (string, error) {
	// 首先尝试使用 os.UserHomeDir()
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		return homeDir, nil
	}

	// 备用方案：使用 os/user 包（不依赖 $HOME 环境变量）
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir, nil
	}

	return "", fmt.Errorf("无法获取用户主目录")
}

// Install 安装 launchd 服务
func (lm *LaunchdManager) Install(config LaunchdConfig) error {
	// 设置默认值
	if config.Label == "" {
		config.Label = lm.label
	}

	// 确保日志目录存在
	if err := os.MkdirAll(config.LogPath, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 确保 LaunchAgents 目录存在
	launchAgentsDir := filepath.Dir(lm.plistPath)
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}

	// 生成 plist 文件
	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return fmt.Errorf("解析模板失败: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("生成 plist 失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(lm.plistPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入 plist 失败: %w", err)
	}

	// 先尝试卸载旧服务（忽略错误，可能从未加载过）
	_ = exec.Command("launchctl", "unload", lm.plistPath).Run()
	if uid := os.Getuid(); uid > 0 {
		target := fmt.Sprintf("gui/%d/%s", uid, lm.label)
		_ = exec.Command("launchctl", "bootout", target).Run()
		// 清除持久化的 disabled 标记（unload/disable 会写入该标记，
		// 不清除的话即使 bootstrap 成功，重启后 launchd 也会跳过该服务）
		_ = exec.Command("launchctl", "enable", target).Run()
	}

	// 使用 launchctl bootstrap 加载服务（推荐方式，比 load 更可靠）
	// 如果 bootstrap 失败，回退到 launchctl load
	loaded := false
	if uid := os.Getuid(); uid > 0 {
		domain := fmt.Sprintf("gui/%d", uid)
		cmd := exec.Command("launchctl", "bootstrap", domain, lm.plistPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			// bootstrap 失败（例如旧 macOS 不支持），回退到 load
			if out, err2 := exec.Command("launchctl", "load", lm.plistPath).CombinedOutput(); err2 != nil {
				return fmt.Errorf("加载服务失败: bootstrap=%s, load=%s", string(output), string(out))
			}
		}
		loaded = true
	}
	if !loaded {
		// uid == 0 的极端情况，直接 load
		cmd := exec.Command("launchctl", "load", lm.plistPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("加载服务失败: %s", string(output))
		}
	}

	return nil
}

// Uninstall 卸载 launchd 服务
func (lm *LaunchdManager) Uninstall() error {
	// 先停止服务
	lm.Stop()

	// 卸载服务（先 bootout，再 unload，忽略错误）
	if uid := os.Getuid(); uid > 0 {
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, lm.label)).Run()
	}
	_ = exec.Command("launchctl", "unload", lm.plistPath).Run()

	// 删除 plist 文件
	if err := os.Remove(lm.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 plist 失败: %w", err)
	}

	return nil
}

// Start 启动服务
func (lm *LaunchdManager) Start() error {
	cmd := exec.Command("launchctl", "start", lm.label)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("启动服务失败: %s", string(output))
	}
	return nil
}

// Stop 停止服务
func (lm *LaunchdManager) Stop() error {
	cmd := exec.Command("launchctl", "stop", lm.label)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("停止服务失败: %s", string(output))
	}
	return nil
}

// Restart 重启服务
func (lm *LaunchdManager) Restart() error {
	// 先停止服务（忽略错误，服务可能未运行）
	lm.Stop()

	// 等待短暂时间让服务完全停止
	time.Sleep(500 * time.Millisecond)

	// 尝试启动服务（忽略命令错误，因为 KeepAlive 可能已经自动重启）
	exec.Command("launchctl", "start", lm.label).Run()

	// 使用重试机制检查服务是否启动成功
	// sbm 是 web 服务，可能需要更多时间启动
	maxRetries := 20
	retryInterval := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		time.Sleep(retryInterval)
		if lm.IsRunning() {
			return nil // 服务启动成功
		}
	}

	return fmt.Errorf("服务重启失败：服务在 %v 内未能启动", time.Duration(maxRetries)*retryInterval)
}

// IsInstalled 检查是否已安装
func (lm *LaunchdManager) IsInstalled() bool {
	_, err := os.Stat(lm.plistPath)
	return err == nil
}

// IsRunning 检查是否运行中
func (lm *LaunchdManager) IsRunning() bool {
	cmd := exec.Command("launchctl", "list", lm.label)
	err := cmd.Run()
	return err == nil
}

// GetPlistPath 获取 plist 文件路径
func (lm *LaunchdManager) GetPlistPath() string {
	return lm.plistPath
}

// GetLabel 获取服务标签
func (lm *LaunchdManager) GetLabel() string {
	return lm.label
}
