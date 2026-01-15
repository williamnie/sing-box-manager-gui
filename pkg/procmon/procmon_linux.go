//go:build linux

package procmon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessStats 进程资源统计
type ProcessStats struct {
	PID        int
	CPUPercent float64
	MemoryMB   float64
}

// 页大小（字节）
var pageSize = int64(os.Getpagesize())

// CPU 时间缓存，用于计算 CPU 百分比
type cpuTimeCache struct {
	utime     uint64
	stime     uint64
	timestamp time.Time
}

var (
	cpuCache   = make(map[int]*cpuTimeCache)
	cpuCacheMu sync.Mutex
)

// GetProcessStats 获取进程资源统计
func GetProcessStats(pid int) (*ProcessStats, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid pid: %d", pid)
	}

	// 检查进程是否存在
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
		return nil, fmt.Errorf("process %d not found", pid)
	}

	stats := &ProcessStats{PID: pid}

	// 获取内存信息
	if memMB, err := getMemoryMB(pid); err == nil {
		stats.MemoryMB = memMB
	}

	// 获取 CPU 使用率
	if cpuPercent, err := getCPUPercent(pid); err == nil {
		stats.CPUPercent = cpuPercent
	}

	return stats, nil
}

// getMemoryMB 从 /proc/{pid}/statm 读取内存信息
func getMemoryMB(pid int) (float64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, fmt.Errorf("invalid statm format")
	}

	// 第二个字段是 RSS（常驻内存页数）
	rssPages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}

	// 转换为 MB
	memoryMB := float64(rssPages*pageSize) / 1024 / 1024
	return memoryMB, nil
}

// getCPUPercent 计算 CPU 使用率
func getCPUPercent(pid int) (float64, error) {
	// 读取进程 CPU 时间
	utime, stime, err := getProcessCPUTime(pid)
	if err != nil {
		return 0, err
	}

	// 获取系统 CPU 核心数
	numCPU := getNumCPU()

	cpuCacheMu.Lock()
	defer cpuCacheMu.Unlock()

	now := time.Now()
	totalTime := utime + stime

	// 检查缓存
	if cached, ok := cpuCache[pid]; ok {
		elapsed := now.Sub(cached.timestamp).Seconds()
		if elapsed > 0 {
			prevTotal := cached.utime + cached.stime
			cpuDelta := float64(totalTime - prevTotal)
			// CPU 时间单位是 clock ticks，通常 100 ticks/秒
			// CPU 百分比 = (cpuDelta / elapsed / 100) * 100 / numCPU
			cpuPercent := (cpuDelta / elapsed) / float64(numCPU)
			
			// 更新缓存
			cached.utime = utime
			cached.stime = stime
			cached.timestamp = now
			
			return cpuPercent, nil
		}
	}

	// 首次调用，初始化缓存
	cpuCache[pid] = &cpuTimeCache{
		utime:     utime,
		stime:     stime,
		timestamp: now,
	}

	return 0, nil
}

// getProcessCPUTime 从 /proc/{pid}/stat 读取 CPU 时间
func getProcessCPUTime(pid int) (utime, stime uint64, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}

	// /proc/pid/stat 格式：pid (comm) state ppid ... utime stime ...
	// utime 是第 14 个字段，stime 是第 15 个字段（从 1 开始计数）
	// 需要处理 comm 中可能包含空格和括号的情况
	content := string(data)
	
	// 找到 comm 结束位置（最后一个 ）
	commEnd := strings.LastIndex(content, ")")
	if commEnd == -1 {
		return 0, 0, fmt.Errorf("invalid stat format")
	}

	// comm 之后的字段
	fields := strings.Fields(content[commEnd+1:])
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("insufficient stat fields")
	}

	// utime 是 comm 之后的第 12 个字段（索引 11）
	// stime 是 comm 之后的第 13 个字段（索引 12）
	utime, err = strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	stime, err = strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	return utime, stime, nil
}

// getNumCPU 获取 CPU 核心数
func getNumCPU() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 1
	}

	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}

	if count == 0 {
		return 1
	}
	return count
}

// CleanupCache 清理已退出进程的缓存
func CleanupCache() {
	cpuCacheMu.Lock()
	defer cpuCacheMu.Unlock()

	for pid := range cpuCache {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
			delete(cpuCache, pid)
		}
	}
}
