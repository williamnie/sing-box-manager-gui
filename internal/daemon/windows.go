//go:build windows

package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Windows 服务配置 XML 模板（用于任务计划程序）
const windowsTaskTemplate = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>SingBox Manager - 代理管理服务</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>{{if .RunAtLoad}}true{{else}}false{{end}}</Enabled>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{{.SbmPath}}</Command>
      <Arguments>-data "{{.DataDir}}" -port {{.Port}}</Arguments>
      <WorkingDirectory>{{.WorkingDir}}</WorkingDirectory>
    </Exec>
  </Actions>
</Task>`

// WindowsConfig Windows 服务配置
type WindowsConfig struct {
	SbmPath    string
	DataDir    string
	Port       string
	LogPath    string
	WorkingDir string
	RunAtLoad  bool
	KeepAlive  bool
}

// WindowsManager Windows 服务管理器
type WindowsManager struct {
	taskName string
	taskPath string // XML 任务文件路径
}

// NewWindowsManager 创建 Windows 服务管理器
func NewWindowsManager() (*WindowsManager, error) {
	taskName := "SingBoxManager"

	homeDir, err := getUserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}

	// 任务配置文件存储位置
	taskPath := filepath.Join(homeDir, ".singbox-manager", "task.xml")

	return &WindowsManager{
		taskName: taskName,
		taskPath: taskPath,
	}, nil
}

// Install 安装 Windows 任务计划
func (wm *WindowsManager) Install(config WindowsConfig) error {
	// 创建日志目录
	if err := os.MkdirAll(config.LogPath, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 创建任务配置目录
	if err := os.MkdirAll(filepath.Dir(wm.taskPath), 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	// 生成任务 XML
	tmpl, err := template.New("windows-task").Parse(windowsTaskTemplate)
	if err != nil {
		return fmt.Errorf("解析模板失败: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("生成任务配置失败: %w", err)
	}

	// 写入 XML 文件
	if err := os.WriteFile(wm.taskPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入任务配置失败: %w", err)
	}

	// 先删除可能存在的旧任务
	wm.runSchtasks("/delete", "/tn", wm.taskName, "/f")

	// 创建任务计划
	if err := wm.runSchtasks("/create", "/tn", wm.taskName, "/xml", wm.taskPath); err != nil {
		return fmt.Errorf("创建任务计划失败: %w", err)
	}

	return nil
}

// Uninstall 卸载 Windows 任务计划
func (wm *WindowsManager) Uninstall() error {
	// 先停止任务
	wm.Stop()

	// 删除任务计划
	if err := wm.runSchtasks("/delete", "/tn", wm.taskName, "/f"); err != nil {
		// 忽略删除失败（可能任务不存在）
	}

	// 删除 XML 文件
	if err := os.Remove(wm.taskPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除任务配置失败: %w", err)
	}

	return nil
}

// Start 启动任务
func (wm *WindowsManager) Start() error {
	return wm.runSchtasks("/run", "/tn", wm.taskName)
}

// Stop 停止任务
func (wm *WindowsManager) Stop() error {
	return wm.runSchtasks("/end", "/tn", wm.taskName)
}

// Restart 重启任务
func (wm *WindowsManager) Restart() error {
	wm.Stop()
	time.Sleep(500 * time.Millisecond)
	wm.Start()

	maxRetries := 20
	for i := 0; i < maxRetries; i++ {
		time.Sleep(500 * time.Millisecond)
		if wm.IsRunning() {
			return nil
		}
	}
	return fmt.Errorf("服务重启失败：服务在 %v 内未能启动", time.Duration(maxRetries)*500*time.Millisecond)
}

// IsInstalled 检查是否已安装
func (wm *WindowsManager) IsInstalled() bool {
	output, err := wm.getSchtasksOutput("/query", "/tn", wm.taskName)
	if err != nil {
		return false
	}
	return strings.Contains(output, wm.taskName)
}

// IsRunning 检查是否运行中
func (wm *WindowsManager) IsRunning() bool {
	output, err := wm.getSchtasksOutput("/query", "/tn", wm.taskName, "/v", "/fo", "list")
	if err != nil {
		return false
	}
	// 检查状态是否为 Running
	return strings.Contains(output, "Running") || strings.Contains(output, "正在运行")
}

// GetTaskPath 获取任务配置文件路径
func (wm *WindowsManager) GetTaskPath() string {
	return wm.taskPath
}

// runSchtasks 执行 schtasks 命令
func (wm *WindowsManager) runSchtasks(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}
	return nil
}

// getSchtasksOutput 执行 schtasks 命令并返回输出
func (wm *WindowsManager) getSchtasksOutput(args ...string) (string, error) {
	cmd := exec.Command("schtasks", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// SetAutoStart 设置开机自启动（通过注册表）
func (wm *WindowsManager) SetAutoStart(sbmPath string, dataDir string, port string) error {
	key, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE,
	)
	if err != nil {
		return fmt.Errorf("打开注册表失败: %w", err)
	}
	defer key.Close()

	// 设置启动命令
	cmd := fmt.Sprintf(`"%s" -data "%s" -port %s`, sbmPath, dataDir, port)
	if err := key.SetStringValue("SingBoxManager", cmd); err != nil {
		return fmt.Errorf("设置注册表值失败: %w", err)
	}

	return nil
}

// RemoveAutoStart 移除开机自启动
func (wm *WindowsManager) RemoveAutoStart() error {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE,
	)
	if err != nil {
		return nil // 键不存在，无需删除
	}
	defer key.Close()

	if err := key.DeleteValue("SingBoxManager"); err != nil {
		// 值不存在，忽略错误
		return nil
	}

	return nil
}
