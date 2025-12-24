//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
)

// isProcessAlive 检查进程是否存活（Windows 实现）
// Windows 不支持 kill -0，使用 gopsutil 检查
func (pm *ProcessManager) isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// 使用 gopsutil 检查进程是否存在
	exists, err := process.PidExists(int32(pid))
	if err != nil {
		return false
	}
	return exists
}

// findSingboxByPgrep 查找 sing-box 进程（Windows 实现）
// Windows 没有 pgrep，使用 tasklist 或 gopsutil
func (pm *ProcessManager) findSingboxByPgrep() int {
	// 方法1：使用 tasklist 命令
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq sing-box.exe", "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if strings.Contains(line, "sing-box.exe") {
				// CSV 格式: "sing-box.exe","1234","Console","1","xxx K"
				parts := strings.Split(line, ",")
				if len(parts) >= 2 {
					pidStr := strings.Trim(parts[1], "\"")
					if pid, err := strconv.Atoi(pidStr); err == nil {
						return pid
					}
				}
			}
		}
	}

	// 方法2：使用 gopsutil 扫描进程
	procs, err := process.Processes()
	if err != nil {
		return 0
	}

	for _, proc := range procs {
		if pm.isSingboxProcess(proc) {
			return int(proc.Pid)
		}
	}

	return 0
}

// sendTermSignal 发送终止信号（Windows 实现：直接 Kill）
// Windows 不支持 SIGTERM，直接终止进程
func (pm *ProcessManager) sendTermSignal(proc *os.Process) error {
	return proc.Kill()
}

// sendReloadSignal 发送重载信号（Windows 实现：不支持，返回错误提示）
// Windows 不支持 SIGHUP，需要重启进程来重载配置
func (pm *ProcessManager) sendReloadSignal(proc *os.Process) error {
	// Windows 不支持热重载，返回 nil 让调用者知道需要重启
	// 实际的重启逻辑在 Reload 方法中处理
	return nil
}
