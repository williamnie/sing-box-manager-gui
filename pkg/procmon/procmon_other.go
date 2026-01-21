//go:build !linux

package procmon

import "fmt"

// ProcessStats 进程资源统计
type ProcessStats struct {
	PID        int
	CPUPercent float64
	MemoryMB   float64
}

// GetProcessStats 非 Linux 平台返回错误
func GetProcessStats(pid int) (*ProcessStats, error) {
	return nil, fmt.Errorf("process monitoring not supported on this platform")
}

// CleanupCache 非 Linux 平台无操作
func CleanupCache() {}
