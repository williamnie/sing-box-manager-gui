package daemon

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

// ProcessManager 进程管理器
type ProcessManager struct {
	singboxPath string
	configPath  string
	dataDir     string // 数据目录，用于设置 sing-box 的工作目录
	pidFile     string // PID 文件路径，用于持久化进程状态
	cmd         *exec.Cmd
	mu          sync.RWMutex
	running     bool
	pid         int // 保存 PID（支持恢复的进程，即使 cmd 为空）
	logs        []string
	maxLogs     int
}

// NewProcessManager 创建进程管理器
func NewProcessManager(singboxPath, configPath, dataDir string) *ProcessManager {
	pm := &ProcessManager{
		singboxPath: singboxPath,
		configPath:  configPath,
		dataDir:     dataDir,
		pidFile:     filepath.Join(dataDir, "singbox.pid"),
		maxLogs:     1000,
		logs:        make([]string, 0),
	}

	// 启动时尝试恢复已有的 sing-box 进程
	pm.recoverProcess()

	return pm
}

// recoverProcess 尝试恢复已有的 sing-box 进程（双重检测）
func (pm *ProcessManager) recoverProcess() {
	var pid int

	// 第一步：尝试从 PID 文件恢复
	pid = pm.recoverFromPidFile()

	// 第二步：如果 PID 文件无效，扫描系统进程
	if pid <= 0 {
		pid = pm.findSingboxProcess()
	}

	if pid <= 0 {
		return // 没有找到 sing-box 进程
	}

	// 恢复状态
	pm.mu.Lock()
	pm.running = true
	pm.pid = pid
	pm.mu.Unlock()

	// 更新 PID 文件（确保一致性）
	os.WriteFile(pm.pidFile, []byte(strconv.Itoa(pid)), 0644)

	logger.Printf("已恢复 sing-box 进程跟踪, PID: %d", pid)

	// 启动异步监控进程退出
	go pm.monitorProcess(pid)
}

// recoverFromPidFile 从 PID 文件恢复（使用 kill -0 快速验证）
func (pm *ProcessManager) recoverFromPidFile() int {
	pid := pm.readPidFile()
	if pid <= 0 {
		return 0
	}

	// 使用 kill -0 快速验证进程是否存活
	if !pm.isProcessAlive(pid) {
		os.Remove(pm.pidFile)
		return 0
	}

	logger.Printf("从 PID 文件恢复 sing-box 进程, PID: %d", pid)
	return pid
}

// findSingboxProcess 使用 pgrep 快速查找 sing-box 进程（启动时使用）
func (pm *ProcessManager) findSingboxProcess() int {
	pid := pm.findSingboxByPgrep()
	if pid > 0 {
		logger.Printf("通过 pgrep 找到 sing-box 进程, PID: %d", pid)
	}
	return pid
}

// isSingboxProcess 检查进程是否是 sing-box
func (pm *ProcessManager) isSingboxProcess(proc *process.Process) bool {
	// 方法1：检查进程名称
	name, _ := proc.Name()
	if name == "sing-box" {
		return true
	}

	// 方法2：检查可执行文件路径（macOS 上进程名可能被截断）
	exe, _ := proc.Exe()
	if strings.HasSuffix(exe, "/sing-box") || strings.HasSuffix(exe, "\\sing-box") {
		return true
	}

	return false
}

// isValidSingboxProcess 验证 PID 是否是有效的 sing-box 进程
func (pm *ProcessManager) isValidSingboxProcess(pid int) bool {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}

	return pm.isSingboxProcess(proc)
}

// readPidFile 只读取 PID 文件，不验证进程类型（轻量级）
func (pm *ProcessManager) readPidFile() int {
	data, err := os.ReadFile(pm.pidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// recoverState 恢复运行状态
func (pm *ProcessManager) recoverState(pid int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		pm.running = true
		pm.pid = pid
		// 更新 PID 文件
		os.WriteFile(pm.pidFile, []byte(strconv.Itoa(pid)), 0644)
		logger.Printf("检测到 sing-box 进程仍在运行，已恢复状态, PID: %d", pid)

		// 重新启动监控
		go pm.monitorProcess(pid)
	}
}

// monitorProcess 监控已恢复的进程（当没有 cmd 对象时使用）
func (pm *ProcessManager) monitorProcess(pid int) {
	failCount := 0
	maxFails := 3 // 连续失败 3 次才认为退出

	for {
		time.Sleep(2 * time.Second)

		// 优先使用 kill -0 检查（更可靠）
		if pm.isProcessAlive(pid) {
			failCount = 0
			continue
		}

		// kill -0 失败，再用 gopsutil 检查
		if pm.isValidSingboxProcess(pid) {
			failCount = 0
			continue
		}

		// 两种方法都失败，计数
		failCount++
		if failCount < maxFails {
			logger.Printf("sing-box 进程检测失败 (%d/%d), PID: %d", failCount, maxFails, pid)
			continue
		}

		// 连续失败达到阈值，认为进程退出
		pm.mu.Lock()
		pm.running = false
		pm.pid = 0
		pm.mu.Unlock()
		os.Remove(pm.pidFile)
		logger.Printf("sing-box 进程已退出, PID: %d", pid)
		return
	}
}

// Start 启动 sing-box
func (pm *ProcessManager) Start() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return fmt.Errorf("sing-box 已经在运行")
	}

	// 检查 sing-box 是否存在
	if _, err := os.Stat(pm.singboxPath); os.IsNotExist(err) {
		return fmt.Errorf("sing-box 不存在: %s", pm.singboxPath)
	}

	// 检查配置文件是否存在
	if _, err := os.Stat(pm.configPath); os.IsNotExist(err) {
		return fmt.Errorf("配置文件不存在: %s", pm.configPath)
	}

	pm.cmd = exec.Command(pm.singboxPath, "run", "-c", pm.configPath)
	pm.cmd.Dir = pm.dataDir // 设置工作目录，确保相对路径（如 external_ui）正确解析

	// 捕获输出
	stdout, err := pm.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取标准输出失败: %w", err)
	}

	stderr, err := pm.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("获取标准错误失败: %w", err)
	}

	if err := pm.cmd.Start(); err != nil {
		return fmt.Errorf("启动 sing-box 失败: %w", err)
	}

	pm.running = true
	pm.pid = pm.cmd.Process.Pid

	// 写入 PID 文件
	if err := os.WriteFile(pm.pidFile, []byte(strconv.Itoa(pm.pid)), 0644); err != nil {
		logger.Printf("写入 PID 文件失败: %v", err)
	}

	logger.Printf("sing-box 已启动, PID: %d", pm.pid)

	// 获取 sing-box 日志记录器
	var singboxLogger *logger.Logger
	if logManager := logger.GetLogManager(); logManager != nil {
		singboxLogger = logManager.SingboxLogger()
	}

	// 异步读取日志
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			pm.addLog(line)
			// 同时写入日志文件
			if singboxLogger != nil {
				singboxLogger.WriteRaw(line)
			}
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			pm.addLog(line)
			// 同时写入日志文件
			if singboxLogger != nil {
				singboxLogger.WriteRaw(line)
			}
		}
	}()

	// 监控进程退出
	go func() {
		pm.cmd.Wait()
		pm.mu.Lock()
		pm.running = false
		pm.pid = 0
		pm.mu.Unlock()
		os.Remove(pm.pidFile)
		logger.Printf("sing-box 进程已退出")
	}()

	return nil
}

// Stop 停止 sing-box
func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return nil
	}

	var pid int

	// 情况1：有 cmd 对象（正常启动的进程）
	if pm.cmd != nil && pm.cmd.Process != nil {
		pid = pm.cmd.Process.Pid
		// 发送终止信号（Unix: SIGTERM, Windows: Kill）
		if err := pm.sendTermSignal(pm.cmd.Process); err != nil {
			// 如果失败，强制 Kill
			if err := pm.cmd.Process.Kill(); err != nil {
				return fmt.Errorf("停止 sing-box 失败: %w", err)
			}
		}
	} else if pm.pid > 0 {
		// 情况2：没有 cmd 对象（恢复的进程），通过 PID 发送信号
		pid = pm.pid
		proc, err := os.FindProcess(pid)
		if err == nil {
			if err := pm.sendTermSignal(proc); err != nil {
				proc.Kill()
			}
		}
	}

	pm.running = false
	pm.pid = 0
	os.Remove(pm.pidFile)
	logger.Printf("sing-box 已停止, PID: %d", pid)
	return nil
}

// Restart 重启 sing-box
func (pm *ProcessManager) Restart() error {
	if err := pm.Stop(); err != nil {
		return err
	}
	return pm.Start()
}

// Reload 热重载配置
func (pm *ProcessManager) Reload() error {
	pm.mu.RLock()
	running := pm.running
	cmd := pm.cmd
	pm.mu.RUnlock()

	if !running {
		return fmt.Errorf("sing-box 未运行")
	}

	// Windows 不支持 SIGHUP，需要重启进程
	if runtime.GOOS == "windows" {
		logger.Printf("Windows 不支持热重载，将重启进程")
		return pm.Restart()
	}

	// Unix 系统：使用 SIGHUP 热重载
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("sing-box 进程对象不可用")
	}

	if err := pm.sendReloadSignal(cmd.Process); err != nil {
		return fmt.Errorf("重载配置失败: %w", err)
	}

	return nil
}

// IsRunning 检查是否运行中（带实时检测和自动恢复）
func (pm *ProcessManager) IsRunning() bool {
	pm.mu.RLock()
	running := pm.running
	pid := pm.pid
	cmd := pm.cmd
	pm.mu.RUnlock()

	// 1. 如果内存状态是运行中，直接返回 true
	if running {
		return true
	}

	// 2. 内存状态是未运行，但尝试实际检测进程是否存活

	// 2.1 检查保存的 PID
	if pid > 0 && pm.isProcessAlive(pid) {
		pm.recoverState(pid)
		return true
	}

	// 2.2 检查 cmd 对象的 PID
	if cmd != nil && cmd.Process != nil {
		cmdPid := cmd.Process.Pid
		if pm.isProcessAlive(cmdPid) {
			pm.recoverState(cmdPid)
			return true
		}
	}

	// 2.3 兜底：从 PID 文件恢复 (读文件 + kill -0，很快)
	if filePid := pm.readPidFile(); filePid > 0 && pm.isProcessAlive(filePid) {
		pm.recoverState(filePid)
		return true
	}

	// 2.4 兜底：用 pgrep 快速查找 (替代 gopsutil 全量扫描)
	if pgrepPid := pm.findSingboxByPgrep(); pgrepPid > 0 {
		pm.recoverState(pgrepPid)
		return true
	}

	return false
}

// GetPID 获取进程 ID
func (pm *ProcessManager) GetPID() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 优先返回保存的 PID（支持恢复的进程）
	if pm.pid > 0 {
		return pm.pid
	}

	// 备用：从 cmd 获取
	if pm.cmd != nil && pm.cmd.Process != nil {
		return pm.cmd.Process.Pid
	}
	return 0
}

// GetLogs 获取日志
func (pm *ProcessManager) GetLogs() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	logs := make([]string, len(pm.logs))
	copy(logs, pm.logs)
	return logs
}

// ClearLogs 清除日志
func (pm *ProcessManager) ClearLogs() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.logs = make([]string, 0)
}

// addLog 添加日志
func (pm *ProcessManager) addLog(line string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logs = append(pm.logs, line)

	// 限制日志数量
	if len(pm.logs) > pm.maxLogs {
		pm.logs = pm.logs[len(pm.logs)-pm.maxLogs:]
	}
}

// SetPaths 设置路径
func (pm *ProcessManager) SetPaths(singboxPath, configPath string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.singboxPath = singboxPath
	pm.configPath = configPath
}

// SetConfigPath 只设置配置文件路径
func (pm *ProcessManager) SetConfigPath(configPath string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.configPath = configPath
}

// Check 检查配置文件
func (pm *ProcessManager) Check() error {
	cmd := exec.Command(pm.singboxPath, "check", "-c", pm.configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("配置检查失败: %s", string(output))
	}
	return nil
}

// Version 获取 sing-box 版本
func (pm *ProcessManager) Version() (string, error) {
	cmd := exec.Command(pm.singboxPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取版本失败: %w", err)
	}
	return string(output), nil
}
