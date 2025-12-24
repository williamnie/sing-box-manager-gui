//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// isProcessAlive 使用 kill -0 检查进程是否存活（Unix 实现）
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

// findSingboxByPgrep 使用 pgrep 快速查找 sing-box 进程（Unix 实现）
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

// sendTermSignal 发送终止信号（Unix 实现：SIGTERM）
func (pm *ProcessManager) sendTermSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

// sendReloadSignal 发送重载信号（Unix 实现：SIGHUP）
func (pm *ProcessManager) sendReloadSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGHUP)
}
