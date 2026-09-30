package kernel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// KernelInfo 内核信息
type KernelInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// DownloadProgress 下载进度
type DownloadProgress struct {
	Status     string  `json:"status"`     // idle, downloading, extracting, installing, completed, error
	Progress   float64 `json:"progress"`   // 0-100
	Message    string  `json:"message"`    // 状态描述
	Downloaded int64   `json:"downloaded"` // 已下载字节
	Total      int64   `json:"total"`      // 总字节
}

// GithubRelease GitHub 发布信息
type GithubRelease struct {
	TagName    string        `json:"tag_name"`
	Name       string        `json:"name"`
	Prerelease bool          `json:"prerelease"`
	Assets     []GithubAsset `json:"assets"`
}

// GithubAsset GitHub 资源文件
type GithubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Manager 内核管理器
type Manager struct {
	dataDir     string
	binPath     string // sing-box 二进制文件的绝对路径
	getSettings func() *storage.Settings
	mu          sync.RWMutex
	installMu   sync.Mutex
	installHook func(string) error
	progress    *DownloadProgress
	downloading bool
}

// SetInstallHook 将最终安装交给进程管理器，保证更新与配置应用互斥并支持健康回退。
func (m *Manager) SetInstallHook(hook func(candidate string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installHook = hook
}

// NewManager 创建内核管理器
func NewManager(dataDir string, getSettings func() *storage.Settings) *Manager {
	// 计算 sing-box 二进制文件的绝对路径
	// dataDir 通常是 ~/.singbox-manager，我们把 sing-box 放在 dataDir/bin/sing-box
	binPath := filepath.Join(dataDir, "bin", "sing-box")

	return &Manager{
		dataDir:     dataDir,
		binPath:     binPath,
		getSettings: getSettings,
		progress: &DownloadProgress{
			Status:  "idle",
			Message: "",
		},
	}
}

// GetInfo 获取内核信息
func (m *Manager) GetInfo() *KernelInfo {
	info := &KernelInfo{
		Path: m.binPath,
		OS:   runtime.GOOS,
		Arch: m.normalizeArch(runtime.GOARCH),
	}

	// 检查文件是否存在
	if _, err := os.Stat(m.binPath); os.IsNotExist(err) {
		info.Installed = false
		return info
	}

	info.Installed = true

	// 获取版本
	version, err := m.getVersion(m.binPath)
	if err == nil {
		info.Version = version
	}

	return info
}

// GetBinPath 获取 sing-box 二进制文件路径
func (m *Manager) GetBinPath() string {
	return m.binPath
}

// getVersion 获取 sing-box 版本
func (m *Manager) getVersion(singboxPath string) (string, error) {
	cmd := exec.Command(singboxPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// 解析版本号，输出格式通常是: sing-box version 1.x.x
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		parts := strings.Fields(lines[0])
		for i, part := range parts {
			if part == "version" && i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
		// 如果没有找到 "version" 关键字，尝试返回第一行
		return strings.TrimSpace(lines[0]), nil
	}

	return "", fmt.Errorf("无法解析版本号")
}

// FetchReleases 获取 GitHub releases 列表
func (m *Manager) FetchReleases() ([]GithubRelease, error) {
	settings := m.getSettings()
	apiURL := "https://api.github.com/repos/SagerNet/sing-box/releases"

	// 如果设置了代理，API 也使用代理
	if settings.GithubProxy != "" {
		apiURL = settings.GithubProxy + apiURL
	}

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("获取 releases 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("GitHub API 请求被限制，请稍后重试或配置代理")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回错误: %d", resp.StatusCode)
	}

	var releases []GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 releases 失败: %w", err)
	}

	// 过滤稳定版本（排除 alpha, beta, rc）
	stablePattern := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	stableReleases := make([]GithubRelease, 0)
	for _, release := range releases {
		if !release.Prerelease && stablePattern.MatchString(release.TagName) {
			stableReleases = append(stableReleases, release)
		}
	}

	return stableReleases, nil
}

// GetLatestVersion 获取最新稳定版本号
func (m *Manager) GetLatestVersion() (string, error) {
	releases, err := m.FetchReleases()
	if err != nil {
		return "", err
	}

	if len(releases) == 0 {
		return "", fmt.Errorf("未找到稳定版本")
	}

	return releases[0].TagName, nil
}

// StartDownload 开始下载指定版本
func (m *Manager) StartDownload(version string) error {
	m.mu.Lock()
	if m.downloading {
		m.mu.Unlock()
		return fmt.Errorf("已有下载任务正在进行")
	}
	m.downloading = true
	m.progress = &DownloadProgress{
		Status:  "preparing",
		Message: "正在准备下载...",
	}
	m.mu.Unlock()

	// 异步执行下载
	go m.downloadAndInstall(version)

	return nil
}

// GetProgress 获取下载进度
func (m *Manager) GetProgress() *DownloadProgress {
	m.mu.RLock()
	defer m.mu.RUnlock()
	progress := *m.progress
	return &progress
}

// IsDownloading 检查是否正在下载
func (m *Manager) IsDownloading() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.downloading
}

// updateProgress 更新进度
func (m *Manager) updateProgress(status string, progress float64, message string, downloaded, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progress = &DownloadProgress{
		Status:     status,
		Progress:   progress,
		Message:    message,
		Downloaded: downloaded,
		Total:      total,
	}
}

// setDownloadComplete 设置下载完成
func (m *Manager) setDownloadComplete(status string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloading = false
	m.progress = &DownloadProgress{
		Status:   status,
		Progress: 100,
		Message:  message,
	}
}

// getAssetInfo 获取对应平台的资源信息
func (m *Manager) getAssetInfo(releases []GithubRelease, version string) (*GithubAsset, error) {
	// 查找对应版本
	var targetRelease *GithubRelease
	for i := range releases {
		if releases[i].TagName == version {
			targetRelease = &releases[i]
			break
		}
	}

	if targetRelease == nil {
		return nil, fmt.Errorf("未找到版本 %s", version)
	}

	// 构建资源文件名
	assetName := m.buildAssetName(version)
	if assetName == "" {
		return nil, fmt.Errorf("不支持的平台: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// 查找对应资源
	for i := range targetRelease.Assets {
		if targetRelease.Assets[i].Name == assetName {
			return &targetRelease.Assets[i], nil
		}
	}

	return nil, fmt.Errorf("未找到适用于 %s/%s 的版本", runtime.GOOS, runtime.GOARCH)
}

// buildAssetName 构建资源文件名
func (m *Manager) buildAssetName(version string) string {
	os := runtime.GOOS
	arch := m.normalizeArch(runtime.GOARCH)
	ver := strings.TrimPrefix(version, "v")

	switch os {
	case "darwin":
		return fmt.Sprintf("sing-box-%s-darwin-%s.tar.gz", ver, arch)
	case "linux":
		return fmt.Sprintf("sing-box-%s-linux-%s.tar.gz", ver, arch)
	case "windows":
		return fmt.Sprintf("sing-box-%s-windows-%s.zip", ver, arch)
	default:
		return ""
	}
}

// normalizeArch 规范化架构名称
func (m *Manager) normalizeArch(arch string) string {
	switch arch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	default:
		return arch
	}
}

// buildDownloadURL 构建下载 URL（支持代理）
func (m *Manager) buildDownloadURL(originalURL string) string {
	settings := m.getSettings()
	if settings.GithubProxy != "" {
		return settings.GithubProxy + originalURL
	}
	return originalURL
}
