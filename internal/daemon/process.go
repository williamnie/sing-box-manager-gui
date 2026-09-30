package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

	"github.com/shirou/gopsutil/v3/process"
	"github.com/xiaobei/singbox-manager/internal/kernel"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

// processIdentity 既校验实例参数，也记录启动时间以拒绝复用的 PID。
type processIdentity struct {
	PID        int    `json:"pid"`
	Created    int64  `json:"created"`
	Executable string `json:"executable"`
	Config     string `json:"config"`
	Directory  string `json:"directory"`
}

// ProcessManager 的 opMu 覆盖完整生命周期/应用事务，mu 仅保护进程状态。
type ProcessManager struct {
	singboxPath, configPath, dataDir, pidFile string
	opMu                                      sync.Mutex
	mu                                        sync.RWMutex
	identity                                  processIdentity
	maxLogs                                   int
	inspect                                   func(int) (processIdentity, error)
	healthWindow, healthTimeout               time.Duration
}

func NewProcessManager(singboxPath, configPath, dataDir string) *ProcessManager {
	// 首次启动可能需要下载规则集；仍要求持续存活和本地 API 就绪，但给冷启动留出时间。
	pm := &ProcessManager{singboxPath: absolutePath(singboxPath), configPath: absolutePath(configPath), dataDir: absolutePath(dataDir), maxLogs: 1000, healthWindow: time.Second, healthTimeout: 30 * time.Second, inspect: inspectProcess}
	pm.pidFile = filepath.Join(pm.dataDir, "singbox.pid")
	pm.recoverProcess()
	return pm
}

func absolutePath(path string) string {
	p, err := filepath.Abs(path)
	if err == nil {
		path = p
	}
	// 新配置尚不存在时仍解析已有父目录（macOS /var -> /private/var）。
	parent := filepath.Clean(path)
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		next := filepath.Dir(parent)
		if next == parent {
			return filepath.Clean(path)
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = next
	}
}

func inspectProcess(pid int) (processIdentity, error) {
	if pid <= 0 {
		return processIdentity{}, fmt.Errorf("无效 PID")
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return processIdentity{}, err
	}
	cwd, err := p.Cwd()
	var exe string
	var args []string
	if err != nil && runtime.GOOS == "darwin" {
		// 官方发布使用 no-cgo，gopsutil 在此构建下没有 Cwd 实现。
		exe, cwd, args, err = inspectDarwinWithoutCGO(pid)
	} else if err == nil {
		exe, err = p.Exe()
		if err == nil {
			args, err = p.CmdlineSlice()
		}
	}
	if err != nil {
		return processIdentity{}, err
	}
	created, err := p.CreateTime()
	if err != nil || created <= 0 {
		return processIdentity{}, fmt.Errorf("无法读取进程启动时间")
	}
	// 仅识别管理器启动的精确参数；不接管 config-directory 或多个配置的实例。
	if len(args) != 4 || args[1] != "run" || args[2] != "-c" {
		return processIdentity{}, fmt.Errorf("进程参数不属于此管理器")
	}
	cfg := args[3]
	if !filepath.IsAbs(cfg) {
		cfg = filepath.Join(cwd, cfg)
	}
	return processIdentity{PID: pid, Created: created, Executable: absolutePath(exe), Config: absolutePath(cfg), Directory: absolutePath(cwd)}, nil
}

func inspectDarwinWithoutCGO(pid int) (string, string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd,txt", "-Ffn").Output()
	if err != nil {
		return "", "", nil, err
	}
	var exe, cwd, fd string
	for _, line := range strings.Split(string(raw), "\n") {
		if len(line) < 2 {
			continue
		}
		if line[0] == 'f' {
			fd = line[1:]
		}
		if line[0] == 'n' && fd == "cwd" {
			cwd = line[1:]
		}
		if line[0] == 'n' && fd == "txt" && exe == "" {
			exe = line[1:]
		}
	}
	if exe == "" || cwd == "" {
		return "", "", nil, fmt.Errorf("无法确认进程可执行文件和工作目录")
	}
	raw, err = exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return "", "", nil, err
	}
	command := strings.TrimSpace(string(raw))
	prefix := exe + " run -c "
	if !strings.HasPrefix(command, prefix) {
		return "", "", nil, fmt.Errorf("进程命令不属于此实例")
	}
	return exe, cwd, []string{exe, "run", "-c", strings.TrimPrefix(command, prefix)}, nil
}

func (pm *ProcessManager) expected(id processIdentity) bool {
	return id.PID > 0 && id.Created > 0 && id.Executable == pm.singboxPath && id.Config == pm.configPath && id.Directory == pm.dataDir
}

func (pm *ProcessManager) matches(id processIdentity) bool {
	if id.PID <= 0 {
		return false
	}
	live, err := pm.inspect(id.PID)
	return err == nil && live == id
}

func (pm *ProcessManager) recoverProcess() {
	raw, err := os.ReadFile(pm.pidFile)
	if err != nil {
		return
	}
	var saved processIdentity
	if json.Unmarshal(raw, &saved) != nil {
		// 兼容旧整数 PID，但必须重新验证全部实例参数。
		saved.PID, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	live, err := pm.inspect(saved.PID)
	if err != nil || !pm.expected(live) || (saved.Created != 0 && saved != live) {
		// 不删除可能属于另一个配置实例的身份文件。
		return
	}
	pm.mu.Lock()
	pm.identity = live
	pm.mu.Unlock()
	if err := pm.saveIdentity(live); err != nil {
		logger.Printf("保存进程身份失败")
	}
	go pm.monitorProcess(live)
}

func (pm *ProcessManager) saveIdentity(id processIdentity) error {
	raw, err := json.Marshal(id)
	if err != nil {
		return err
	}
	return atomicWrite(pm.pidFile, raw, 0600)
}

func (pm *ProcessManager) currentIdentity() processIdentity {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.identity
}

func (pm *ProcessManager) clearIdentity(id processIdentity) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.identity != id {
		return
	}
	pm.identity = processIdentity{}
	// 旧监视器退出不能删除后来启动的实例所写的新 PID 文件。
	if raw, err := os.ReadFile(pm.pidFile); err == nil {
		var saved processIdentity
		if json.Unmarshal(raw, &saved) == nil && saved == id {
			_ = os.Remove(pm.pidFile)
		}
	}
}

func (pm *ProcessManager) monitorProcess(id processIdentity) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if pm.currentIdentity() != id {
			return
		}
		if !pm.matches(id) {
			pm.clearIdentity(id)
			return
		}
	}
}

func (pm *ProcessManager) running() bool {
	id := pm.currentIdentity()
	if pm.matches(id) {
		return true
	}
	if id.PID != 0 {
		pm.clearIdentity(id)
	}
	return false
}

func (pm *ProcessManager) start() error {
	if pm.running() {
		return fmt.Errorf("sing-box 已经在运行")
	}
	_, err := os.Stat(pm.configPath)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	cmd := exec.Command(pm.singboxPath, "run", "-c", pm.configPath)
	cmd.Dir = pm.dataDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := pm.startLogRelay()
	if err != nil {
		return fmt.Errorf("启动独立日志转发失败: %w", err)
	}
	defer output.Close()
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 sing-box 失败: %w", err)
	}
	// 只对刚启动且持有句柄的子进程进行失败清理。
	id, err := pm.inspect(cmd.Process.Pid)
	if err != nil || !pm.expected(id) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("启动后的进程身份验证失败: %v", err)
	}
	pm.mu.Lock()
	pm.identity = id
	pm.mu.Unlock()
	if err := pm.saveIdentity(id); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		pm.clearIdentity(id)
		return fmt.Errorf("保存进程身份失败: %w", err)
	}
	go func() { _ = cmd.Wait(); pm.clearIdentity(id) }()
	return nil
}

func (pm *ProcessManager) signal(id processIdentity, signal syscall.Signal) error {
	proc, err := os.FindProcess(id.PID)
	if err != nil {
		return err
	}
	defer proc.Release()
	if !pm.matches(id) {
		return fmt.Errorf("进程身份已改变，拒绝发送信号")
	}
	return proc.Signal(signal)
}

func (pm *ProcessManager) stop() error {
	id := pm.currentIdentity()
	if id.PID == 0 {
		return nil
	}
	if !pm.matches(id) {
		pm.clearIdentity(id)
		return nil
	}
	if err := pm.signal(id, syscall.SIGTERM); err != nil {
		return fmt.Errorf("停止 sing-box 失败: %w", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !pm.matches(id) {
			pm.clearIdentity(id)
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := pm.signal(id, syscall.SIGKILL); err != nil {
		return fmt.Errorf("终止 sing-box 失败: %w", err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !pm.matches(id) {
			pm.clearIdentity(id)
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("sing-box 停止超时")
}

func (pm *ProcessManager) Start() error {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	if err := pm.checkPath(pm.singboxPath, pm.configPath); err != nil {
		return err
	}
	if err := pm.start(); err != nil {
		return err
	}
	if err := pm.healthy(); err != nil {
		stopErr := pm.stop()
		return errors.Join(err, stopErr)
	}
	return nil
}
func (pm *ProcessManager) Stop() error { pm.opMu.Lock(); defer pm.opMu.Unlock(); return pm.stop() }
func (pm *ProcessManager) Restart() error {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	return pm.restart()
}
func (pm *ProcessManager) restart() error {
	if pm.running() && !pm.expected(pm.currentIdentity()) {
		return fmt.Errorf("实例路径已变更，请先停止原实例后再启动")
	}
	if err := pm.checkPath(pm.singboxPath, pm.configPath); err != nil {
		return err
	}
	if err := pm.stop(); err != nil {
		return err
	}
	if err := pm.start(); err != nil {
		return err
	}
	if err := pm.healthy(); err != nil {
		return errors.Join(err, pm.stop())
	}
	return nil
}
func (pm *ProcessManager) Reload() error {
	// 重启经相同健康检查；避免未经验证的 SIGHUP 在旧版内核中终止服务。
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	if !pm.running() {
		return fmt.Errorf("sing-box 未运行")
	}
	return pm.restart()
}
func (pm *ProcessManager) IsRunning() bool {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	return pm.running()
}
func (pm *ProcessManager) GetPID() int {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	if !pm.running() {
		return 0
	}
	return pm.currentIdentity().PID
}
func (pm *ProcessManager) GetLogs() []string {
	f, err := os.Open(filepath.Join(pm.dataDir, "logs", "singbox.log"))
	if err != nil {
		return []string{}
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > pm.maxLogs {
			lines = lines[len(lines)-pm.maxLogs:]
		}
	}
	return lines
}
func (pm *ProcessManager) ClearLogs() {
	// 日志转发器以 O_APPEND 打开，截断不会破坏其后续写入。
	_ = os.Truncate(filepath.Join(pm.dataDir, "logs", "singbox.log"), 0)
}

// 运行中的实例保留启动身份，后续停止时仍只操作该实例。
func (pm *ProcessManager) SetPaths(binary, config string) {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	pm.singboxPath = absolutePath(binary)
	pm.configPath = absolutePath(config)
}
func (pm *ProcessManager) SetConfigPath(config string) {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	pm.configPath = absolutePath(config)
}

func (pm *ProcessManager) checkPath(binary, config string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "check", "-c", config)
	cmd.Dir = pm.dataDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		raw, _ := os.ReadFile(config)
		detail := logger.Redact(configRedactor(raw)(strings.TrimSpace(string(output))))
		if len(detail) > 2048 {
			detail = detail[:2048]
		}
		return fmt.Errorf("配置检查失败: %w: %s", err, detail)
	}
	return nil
}

func (pm *ProcessManager) Check() error {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	return pm.checkPath(pm.singboxPath, pm.configPath)
}
func (pm *ProcessManager) CheckConfig(raw []byte) error {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	return pm.checkCandidate(pm.singboxPath, raw)
}
func (pm *ProcessManager) checkCandidate(binary string, raw []byte) error {
	if !json.Valid(raw) {
		return fmt.Errorf("配置必须是合法 JSON")
	}
	if err := os.MkdirAll(filepath.Dir(pm.configPath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(pm.configPath), ".candidate-*.json")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return pm.checkPath(binary, path)
}

// ApplyConfig 是手动和自动应用的唯一事务入口。停止状态只验证和落盘，不隐式启动。
func (pm *ProcessManager) ApplyConfig(raw []byte) error {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	if pm.running() && !pm.expected(pm.currentIdentity()) {
		return fmt.Errorf("实例路径已变更，请先停止原实例后再应用")
	}
	if err := pm.checkCandidate(pm.singboxPath, raw); err != nil {
		return err
	}
	old, err := os.ReadFile(pm.configPath)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if existed {
		if err := atomicWrite(pm.configPath+".previous", old, 0600); err != nil {
			return fmt.Errorf("备份配置失败: %w", err)
		}
	}
	wasRunning := pm.running()
	if err := atomicWrite(pm.configPath, raw, 0600); err != nil {
		// rename 后的目录 fsync 也可能失败，此时同样恢复旧文件。
		if existed {
			return errors.Join(err, atomicWrite(pm.configPath, old, 0600))
		}
		removeErr := os.Remove(pm.configPath)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return errors.Join(err, removeErr)
	}
	if !wasRunning {
		return nil
	}
	applyErr := pm.stop()
	if applyErr == nil {
		applyErr = pm.start()
	}
	if applyErr == nil {
		applyErr = pm.healthy()
	}
	if applyErr == nil {
		return nil
	}
	stopErr := pm.stop()
	if existed {
		err = atomicWrite(pm.configPath, old, 0600)
	} else {
		err = os.Remove(pm.configPath)
	}
	if err != nil {
		return errors.Join(applyErr, stopErr, fmt.Errorf("恢复配置失败: %w", err))
	}
	if stopErr != nil {
		return errors.Join(fmt.Errorf("原配置已恢复，但候选进程停止失败: %w", applyErr), stopErr)
	}
	if existed {
		if err = pm.start(); err == nil {
			err = pm.healthy()
		}
	}
	if err != nil {
		return errors.Join(applyErr, fmt.Errorf("旧配置已恢复，但恢复运行失败: %w", err))
	}
	return fmt.Errorf("候选配置应用失败，已恢复原配置和进程: %w", applyErr)
}

func atomicWrite(path string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".atomic-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (pm *ProcessManager) healthy() error {
	raw, err := os.ReadFile(pm.configPath)
	if err != nil {
		return err
	}
	var cfg struct {
		Experimental struct {
			Clash struct {
				Controller string `json:"external_controller"`
				Secret     string `json:"secret"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	endpoint := ""
	if controller := cfg.Experimental.Clash.Controller; controller != "" {
		host, port, err := net.SplitHostPort(controller)
		if err != nil {
			return fmt.Errorf("Clash API 地址格式错误")
		}
		ip := net.ParseIP(host)
		if host != "" && host != "localhost" && (ip == nil || (!ip.IsLoopback() && !ip.IsUnspecified())) {
			return fmt.Errorf("健康检查仅允许本地 Clash API")
		}
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		endpoint = "http://" + net.JoinHostPort(host, port) + "/version"
	}
	client := &http.Client{Timeout: 250 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	start := time.Now()
	deadline := start.Add(pm.healthTimeout)
	for time.Now().Before(deadline) {
		if !pm.running() {
			return fmt.Errorf("sing-box 未能持续运行")
		}
		ready := endpoint == ""
		if endpoint != "" {
			req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
			if cfg.Experimental.Clash.Secret != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.Experimental.Clash.Secret)
			}
			resp, err := client.Do(req)
			if err == nil {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				var v struct {
					Version string `json:"version"`
				}
				ready = resp.StatusCode == http.StatusOK && json.Unmarshal(body, &v) == nil && v.Version != ""
			}
		}
		if ready && time.Since(start) >= pm.healthWindow {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("sing-box 健康检查超时")
}

func configRedactor(raw []byte) func(string) string {
	var config any
	_ = json.Unmarshal(raw, &config)
	var secrets []string
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if logger.SensitiveKey(key) {
					if s, ok := child.(string); ok && s != "" {
						secrets = append(secrets, s)
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(config)
	return func(line string) string {
		for _, secret := range secrets {
			line = strings.ReplaceAll(line, secret, "[REDACTED]")
			quoted, _ := json.Marshal(secret)
			line = strings.ReplaceAll(line, string(quoted), `"[REDACTED]"`)
		}
		return line
	}
}

func (pm *ProcessManager) Version() (string, error) {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, pm.singboxPath, "version").Output()
	return string(out), err
}

// InstallKernel 供 kernel.Manager 的安装 hook 使用，与配置事务串行。
func (pm *ProcessManager) InstallKernel(candidate string) error {
	pm.opMu.Lock()
	defer pm.opMu.Unlock()
	if pm.running() && !pm.expected(pm.currentIdentity()) {
		return fmt.Errorf("实例路径已变更，请先停止原实例后再更新内核")
	}
	if err := kernel.ValidateBinary(candidate); err != nil {
		return err
	}
	if _, err := os.Stat(pm.configPath); err == nil {
		if err := pm.checkPath(candidate, pm.configPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	wasRunning := pm.running()
	if wasRunning {
		if err := pm.stop(); err != nil {
			return err
		}
	}
	rollback, err := kernel.ReplaceBinary(candidate, pm.singboxPath)
	if err != nil {
		if wasRunning {
			recovery := pm.start()
			if recovery == nil {
				recovery = pm.healthy()
			}
			return errors.Join(err, recovery)
		}
		return err
	}
	if !wasRunning {
		return nil
	}
	if err = pm.start(); err == nil {
		err = pm.healthy()
	}
	if err == nil {
		return nil
	}
	if stopErr := pm.stop(); stopErr != nil {
		return errors.Join(err, stopErr)
	}
	if restoreErr := rollback(); restoreErr != nil {
		return errors.Join(err, fmt.Errorf("恢复内核失败: %w", restoreErr))
	}
	recovery := pm.start()
	if recovery == nil {
		recovery = pm.healthy()
	}
	if recovery != nil {
		return errors.Join(err, fmt.Errorf("内核已恢复但启动失败: %w", recovery))
	}
	return fmt.Errorf("新内核未通过健康检查，已恢复旧内核: %w", err)
}

// RunLogRelay 仅供主程序固定内部模式调用。独立进程持有管道读端，使管理器退出不触发内核 SIGPIPE。
// 转发器不接受命令；配置、日志均限制在同一数据目录，stdin EOF 后自动退出。
func RunLogRelay(dataDir, configPath string) error {
	dataDir = absolutePath(dataDir)
	configPath = absolutePath(configPath)
	rel, err := filepath.Rel(dataDir, configPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("日志配置不属于数据目录")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	redact := configRedactor(raw)
	f, err := os.OpenFile(filepath.Join(dataDir, "singbox-relay.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("旧日志转发器尚未结束")
		}
		time.Sleep(20 * time.Millisecond)
	}
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return err
	}
	logFile, err := logger.NewLogger(filepath.Join(logDir, "singbox.log"), "")
	if err != nil {
		return err
	}
	defer logFile.Close()
	if err := os.Chmod(filepath.Join(logDir, "singbox.log"), 0600); err != nil {
		return err
	}
	ready := os.NewFile(3, "relay-ready")
	if ready == nil {
		return fmt.Errorf("缺少内部就绪通道")
	}
	if _, err := ready.Write([]byte{1}); err != nil {
		ready.Close()
		return err
	}
	ready.Close()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		logFile.WriteRaw(logger.Redact(redact(scanner.Text())))
	}
	// 超长日志行不应堵塞内核 stdout：丢弃剩余输入直到内核退出。
	if err := scanner.Err(); err != nil {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return fmt.Errorf("日志行超出限制")
	}
	return nil
}

func (pm *ProcessManager) startLogRelay() (*os.File, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	input, output, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	relay := exec.Command(executable, "--internal-log-relay", pm.dataDir, pm.configPath)
	relay.Dir = pm.dataDir
	relay.Stdin = input
	relay.ExtraFiles = []*os.File{readyWrite}
	relay.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = relay.Start()
	input.Close()
	readyWrite.Close()
	if err != nil {
		readyRead.Close()
		output.Close()
		return nil, err
	}
	go func() { _ = relay.Wait() }()
	defer readyRead.Close()
	ready := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := io.ReadFull(readyRead, b[:])
		if err == nil && b[0] != 1 {
			err = fmt.Errorf("日志转发器应答无效")
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			output.Close()
			return nil, err
		}
		return output, nil
	case <-time.After(3 * time.Second):
		output.Close()
		return nil, fmt.Errorf("日志转发器就绪超时")
	}
}
