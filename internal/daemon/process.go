package daemon

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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

	// Ring buffer 日志存储（固定内存，避免频繁分配）
	logs     []string
	logHead  int // 下一个写入位置
	logCount int // 当前日志数量
	maxLogs  int
}

// NewProcessManager 创建进程管理器
func NewProcessManager(singboxPath, configPath, dataDir string) *ProcessManager {
	maxLogs := 1000
	pm := &ProcessManager{
		singboxPath: singboxPath,
		configPath:  configPath,
		dataDir:     dataDir,
		pidFile:     filepath.Join(dataDir, "singbox.pid"),
		maxLogs:     maxLogs,
		logs:        make([]string, maxLogs), // 预分配固定大小
		logHead:     0,
		logCount:    0,
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

// isValidSingboxProcess 验证 PID 是否是有效的 sing-box 进程
func (pm *ProcessManager) isValidSingboxProcess(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Linux: 读取 /proc/{pid}/comm 获取进程名
	if runtime.GOOS == "linux" {
		// 方法1：检查进程名称
		commPath := fmt.Sprintf("/proc/%d/comm", pid)
		if data, err := os.ReadFile(commPath); err == nil {
			name := strings.TrimSpace(string(data))
			if name == "sing-box" {
				return true
			}
		}

		// 方法2：检查可执行文件路径
		exePath := fmt.Sprintf("/proc/%d/exe", pid)
		if exe, err := os.Readlink(exePath); err == nil {
			if strings.HasSuffix(exe, "/sing-box") {
				return true
			}
		}
	}

	return false
}

// isProcessAlive 使用 kill -0 检查进程是否存活（更可靠）
func (pm *ProcessManager) isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// kill -0 不发送信号，只检查进程是否存在
	err = proc.Signal(syscall.Signal(0))
	return err == nil
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

// findSingboxByPgrep 使用 pgrep 快速查找 sing-box 进程
func (pm *ProcessManager) findSingboxByPgrep() int {
	// pgrep -x 精确匹配进程名
	cmd := exec.Command("pgrep", "-x", "sing-box")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// pgrep 可能返回多行（多个进程），取第一个
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return 0
	}

	pid, err := strconv.Atoi(lines[0])
	if err != nil {
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

		// kill -0 失败，再用 /proc 检查进程是否是 sing-box
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
		// 发送 SIGTERM 信号
		if err := pm.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// 如果 SIGTERM 失败，尝试 SIGKILL
			if err := pm.cmd.Process.Kill(); err != nil {
				return fmt.Errorf("停止 sing-box 失败: %w", err)
			}
		}
	} else if pm.pid > 0 {
		// 情况2：没有 cmd 对象（恢复的进程），通过 PID 发送信号
		pid = pm.pid
		proc, err := os.FindProcess(pid)
		if err == nil {
			if err := proc.Signal(syscall.SIGTERM); err != nil {
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
	defer pm.mu.RUnlock()

	if !pm.running || pm.cmd == nil || pm.cmd.Process == nil {
		return fmt.Errorf("sing-box 未运行")
	}

	// sing-box 支持 SIGHUP 热重载
	if err := pm.cmd.Process.Signal(syscall.SIGHUP); err != nil {
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

	// 2.4 兜底：用 pgrep 快速查找
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

// GetLogs 获取日志（从 ring buffer 按时间顺序返回）
func (pm *ProcessManager) GetLogs() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.logCount == 0 {
		return []string{}
	}

	result := make([]string, pm.logCount)
	if pm.logCount < pm.maxLogs {
		// 未满，直接从头开始复制
		copy(result, pm.logs[:pm.logCount])
	} else {
		// 已满，从 logHead 位置开始是最旧的日志
		copy(result, pm.logs[pm.logHead:])
		copy(result[pm.maxLogs-pm.logHead:], pm.logs[:pm.logHead])
	}
	return result
}

// ClearLogs 清除日志
func (pm *ProcessManager) ClearLogs() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 清零所有槽位，释放字符串引用
	for i := range pm.logs {
		pm.logs[i] = ""
	}
	pm.logHead = 0
	pm.logCount = 0
}

// addLog 添加日志（ring buffer 实现）
func (pm *ProcessManager) addLog(line string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 写入当前位置
	pm.logs[pm.logHead] = line
	pm.logHead = (pm.logHead + 1) % pm.maxLogs

	if pm.logCount < pm.maxLogs {
		pm.logCount++
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

// HealthChecker 健康检查器
type HealthChecker struct {
	pm              *ProcessManager
	clashAPIPort    int
	clashAPISecret  string
	interval        time.Duration
	autoRestart     bool
	enabled         bool
	stopCh          chan struct{}
	mu              sync.Mutex
	failCount       int
	maxFails        int
	lastCheckTime   time.Time
	lastCheckResult bool
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(pm *ProcessManager) *HealthChecker {
	return &HealthChecker{
		pm:       pm,
		interval: 30 * time.Second,
		maxFails: 3,
		stopCh:   make(chan struct{}),
	}
}

// Configure 配置健康检查器
func (hc *HealthChecker) Configure(enabled bool, interval int, autoRestart bool, clashAPIPort int, clashAPISecret string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.enabled = enabled
	if interval > 0 {
		hc.interval = time.Duration(interval) * time.Second
	}
	hc.autoRestart = autoRestart
	hc.clashAPIPort = clashAPIPort
	hc.clashAPISecret = clashAPISecret
}

// Start 启动健康检查
func (hc *HealthChecker) Start() {
	hc.mu.Lock()
	if !hc.enabled {
		hc.mu.Unlock()
		return
	}
	hc.stopCh = make(chan struct{})
	hc.mu.Unlock()

	go hc.run()
}

// Stop 停止健康检查
func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.stopCh != nil {
		close(hc.stopCh)
		hc.stopCh = nil
	}
}

// run 健康检查循环
func (hc *HealthChecker) run() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	logger.Printf("健康检查已启动，间隔: %v", hc.interval)

	for {
		select {
		case <-hc.stopCh:
			logger.Printf("健康检查已停止")
			return
		case <-ticker.C:
			hc.check()
		}
	}
}

// check 执行健康检查
func (hc *HealthChecker) check() {
	hc.mu.Lock()
	clashAPIPort := hc.clashAPIPort
	clashAPISecret := hc.clashAPISecret
	autoRestart := hc.autoRestart
	hc.mu.Unlock()

	// 如果进程未运行，跳过检查
	if !hc.pm.IsRunning() {
		return
	}

	// 请求 Clash API
	healthy := hc.checkClashAPI(clashAPIPort, clashAPISecret)

	hc.mu.Lock()
	hc.lastCheckTime = time.Now()
	hc.lastCheckResult = healthy

	if healthy {
		hc.failCount = 0
		hc.mu.Unlock()
		return
	}

	hc.failCount++
	failCount := hc.failCount
	hc.mu.Unlock()

	logger.Printf("健康检查失败 (%d/%d)", failCount, hc.maxFails)

	// 连续失败达到阈值，尝试重启
	if failCount >= hc.maxFails && autoRestart {
		logger.Printf("健康检查连续失败 %d 次，正在重启 sing-box...", failCount)
		if err := hc.pm.Restart(); err != nil {
			logger.Printf("自动重启失败: %v", err)
		} else {
			logger.Printf("自动重启成功")
			hc.mu.Lock()
			hc.failCount = 0
			hc.mu.Unlock()
		}
	}
}

// checkClashAPI 检查 Clash API 是否响应
func (hc *HealthChecker) checkClashAPI(port int, secret string) bool {
	if port <= 0 {
		return true // 没配置端口，跳过检查
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/version", port)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}

	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// GetStatus 获取健康检查状态
func (hc *HealthChecker) GetStatus() map[string]interface{} {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	return map[string]interface{}{
		"enabled":           hc.enabled,
		"interval":          int(hc.interval.Seconds()),
		"auto_restart":      hc.autoRestart,
		"fail_count":        hc.failCount,
		"last_check_time":   hc.lastCheckTime,
		"last_check_result": hc.lastCheckResult,
	}
}
