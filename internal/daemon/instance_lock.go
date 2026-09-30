//go:build darwin || linux

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// InstanceLock 持有操作系统文件锁；崩溃退出后内核自动释放，禁止删除锁文件。
type InstanceLock struct {
	file     *os.File
	once     sync.Once
	closeErr error
}

func AcquireInstanceLock(dataDir string) (*InstanceLock, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	dataDir = absolutePath(dataDir)
	path := filepath.Join(dataDir, "manager.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开管理器实例锁失败: %w", err)
	}
	fail := func(err error) (*InstanceLock, error) { f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(fmt.Errorf("实例锁不是普通文件"))
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("此数据目录已有管理器运行，无法获得实例锁: %w", err))
	}
	if err := f.Chmod(0600); err != nil {
		return fail(err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	record := struct {
		PID        int       `json:"pid"`
		Executable string    `json:"executable"`
		Directory  string    `json:"directory"`
		Started    time.Time `json:"started"`
	}{os.Getpid(), absolutePath(exe), dataDir, time.Now().UTC()}
	raw, err := json.Marshal(record)
	if err != nil {
		return fail(err)
	}
	if err := f.Truncate(0); err != nil {
		return fail(err)
	}
	if _, err := f.WriteAt(raw, 0); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	return &InstanceLock{file: f}, nil
}

func (l *InstanceLock) Close() error {
	l.once.Do(func() { l.closeErr = l.file.Close() })
	return l.closeErr
}
