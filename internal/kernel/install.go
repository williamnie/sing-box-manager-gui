package kernel

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Capabilities 显式区分已支持的能力，生成配置仍须由该版本 check 验证。
type Capabilities struct {
	Version      string `json:"version"`
	AutoRedirect bool   `json:"auto_redirect"`
	TypedDNS     bool   `json:"typed_dns"`
	Gateway      bool   `json:"gateway"`
}

var versionPattern = regexp.MustCompile(`(?:^|\s)(?:v)?(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?(?:\s|$)`)

func CapabilitiesFor(version string) (Capabilities, error) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) == 0 {
		return Capabilities{}, fmt.Errorf("无法解析 sing-box 版本")
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major != 1 || minor < 8 {
		return Capabilities{}, fmt.Errorf("不支持的 sing-box 版本（要求 1.8 或更高的 1.x 版本）")
	}
	return Capabilities{Version: strings.TrimSpace(match[0]), AutoRedirect: minor >= 10, TypedDNS: minor >= 12, Gateway: minor >= 14}, nil
}

func ValidateBinary(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("候选内核版本检查失败: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "sing-box version ") {
		return fmt.Errorf("候选文件不是 sing-box 内核")
	}
	_, err = CapabilitiesFor(strings.TrimPrefix(strings.SplitN(string(raw), "\n", 2)[0], "sing-box version "))
	return err
}

// ReplaceBinary 候选复制和持久化成功后原子切换；返回的 rollback 可恢复之前的版本。
// 调用者负责候选校验、停止旧进程以及切换后的健康验证。
func ReplaceBinary(candidate, target string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return nil, err
	}
	staged, err := copyStaged(candidate, filepath.Dir(target))
	if err != nil {
		return nil, err
	}
	defer os.Remove(staged)
	backup := target + ".previous"
	existed := false
	if _, err := os.Stat(target); err == nil {
		existed = true
		old, err := copyStaged(target, filepath.Dir(target))
		if err != nil {
			return nil, fmt.Errorf("备份内核失败: %w", err)
		}
		defer os.Remove(old)
		if err := os.Rename(old, backup); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Rename(staged, target); err != nil {
		return nil, err
	}
	rollback := func() error {
		if !existed {
			return os.Remove(target)
		}
		restored, err := copyStaged(backup, filepath.Dir(target))
		if err != nil {
			return err
		}
		defer os.Remove(restored)
		if err := os.Rename(restored, target); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		restoreErr := rollback()
		return nil, fmt.Errorf("持久化内核失败: %v; 恢复结果: %v", err, restoreErr)
	}
	return rollback, nil
}

func copyStaged(source, dir string) (string, error) {
	src, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("内核候选不是普通文件")
	}
	dst, err := os.CreateTemp(dir, ".kernel-*")
	if err != nil {
		return "", err
	}
	path := dst.Name()
	if _, err = io.Copy(dst, src); err == nil {
		err = dst.Chmod(0755)
	}
	if err == nil {
		err = dst.Sync()
	}
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
