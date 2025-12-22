package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"
)

const systemdTemplate = `[Unit]
Description=SingBox Manager
After=network.target

[Service]
Type=simple
ExecStart={{.SbmPath}} -data {{.DataDir}} -port {{.Port}}
WorkingDirectory={{.WorkingDir}}
Restart={{if .KeepAlive}}always{{else}}no{{end}}
RestartSec=5
StandardOutput=append:{{.LogPath}}/sbm.log
StandardError=append:{{.LogPath}}/sbm.error.log
Environment="HOME={{.HomeDir}}"

[Install]
WantedBy={{if .RunAtLoad}}default.target{{else}}multi-user.target{{end}}
`

// SystemdConfig systemd 配置
type SystemdConfig struct {
	SbmPath    string
	DataDir    string
	Port       string
	LogPath    string
	WorkingDir string
	HomeDir    string
	RunAtLoad  bool
	KeepAlive  bool
}

// SystemdManager systemd 管理器
type SystemdManager struct {
	serviceName string
	servicePath string
	userMode    bool
}

// NewSystemdManager 创建 systemd 管理器
func NewSystemdManager() (*SystemdManager, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("systemd 仅在 Linux 上支持")
	}

	serviceName := "singbox-manager.service"
	homeDir, err := getUserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}

	// 用户级服务路径
	servicePath := filepath.Join(homeDir, ".config", "systemd", "user", serviceName)

	return &SystemdManager{
		serviceName: serviceName,
		servicePath: servicePath,
		userMode:    true,
	}, nil
}

// Install 安装 systemd 服务
func (sm *SystemdManager) Install(config SystemdConfig) error {
	if err := os.MkdirAll(config.LogPath, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(sm.servicePath), 0755); err != nil {
		return fmt.Errorf("创建 systemd 目录失败: %w", err)
	}

	tmpl, err := template.New("systemd").Parse(systemdTemplate)
	if err != nil {
		return fmt.Errorf("解析模板失败: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("生成 service 文件失败: %w", err)
	}

	if err := os.WriteFile(sm.servicePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入 service 文件失败: %w", err)
	}

	// 重新加载 systemd 配置
	if err := sm.runSystemctl("daemon-reload"); err != nil {
		return fmt.Errorf("重新加载配置失败: %w", err)
	}

	// 启用服务（开机自启）
	if config.RunAtLoad {
		if err := sm.runSystemctl("enable", sm.serviceName); err != nil {
			return fmt.Errorf("启用服务失败: %w", err)
		}
	}

	return nil
}

// Uninstall 卸载 systemd 服务
func (sm *SystemdManager) Uninstall() error {
	sm.Stop()
	sm.runSystemctl("disable", sm.serviceName)

	if err := os.Remove(sm.servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 service 文件失败: %w", err)
	}

	sm.runSystemctl("daemon-reload")
	return nil
}

// Start 启动服务
func (sm *SystemdManager) Start() error {
	return sm.runSystemctl("start", sm.serviceName)
}

// Stop 停止服务
func (sm *SystemdManager) Stop() error {
	return sm.runSystemctl("stop", sm.serviceName)
}

// Restart 重启服务
func (sm *SystemdManager) Restart() error {
	sm.Stop()
	time.Sleep(500 * time.Millisecond)
	sm.runSystemctl("start", sm.serviceName)

	maxRetries := 20
	for i := 0; i < maxRetries; i++ {
		time.Sleep(500 * time.Millisecond)
		if sm.IsRunning() {
			return nil
		}
	}
	return fmt.Errorf("服务重启失败：服务在 %v 内未能启动", time.Duration(maxRetries)*500*time.Millisecond)
}

// IsInstalled 检查是否已安装
func (sm *SystemdManager) IsInstalled() bool {
	_, err := os.Stat(sm.servicePath)
	return err == nil
}

// IsRunning 检查是否运行中
func (sm *SystemdManager) IsRunning() bool {
	err := sm.runSystemctl("is-active", "--quiet", sm.serviceName)
	return err == nil
}

// GetServicePath 获取 service 文件路径
func (sm *SystemdManager) GetServicePath() string {
	return sm.servicePath
}

func (sm *SystemdManager) runSystemctl(args ...string) error {
	if sm.userMode {
		args = append([]string{"--user"}, args...)
	}
	cmd := exec.Command("systemctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}
	return nil
}
