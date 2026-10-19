package api

import (
	"os"
	"os/exec"
	"strings"
)

// readBootID 区分管理器重启与主机重启；取不到时返回空串，恢复按跨启动的保守路径处理。
func readBootID() string {
	if raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		return strings.TrimSpace(string(raw))
	}
	if out, err := exec.Command("/usr/sbin/sysctl", "-n", "kern.bootsessionuuid").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}
