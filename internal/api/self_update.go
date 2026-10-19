package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

const (
	selfUpdateLatestAPI = "https://api.github.com/repos/williamnie/sing-box-manager-gui/releases/latest"
	selfUpdateInterval  = 12 * time.Hour
	selfUpdateFirstWait = time.Minute
	selfUpdateNotesMax  = 8 << 10
)

// selfUpdateRelease 是 GitHub 最新正式发布（/releases/latest 不含预发布与草稿）。
type selfUpdateRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// selfUpdater 只负责检测与提示；下载替换仅由用户在界面上显式触发。
type selfUpdater struct {
	mu         sync.Mutex
	current    string
	latest     *selfUpdateRelease
	checkedAt  time.Time
	lastErr    string
	checking   bool
	installing bool
	client     *http.Client
	latestAPI  string
	release    string
	stop       chan struct{}
	done       chan struct{}
}

func newSelfUpdater(version string) *selfUpdater {
	return &selfUpdater{
		current:   strings.TrimPrefix(version, "v"),
		client:    &http.Client{Timeout: 5 * time.Minute},
		latestAPI: selfUpdateLatestAPI,
		release:   agentClientRelease,
	}
}

// compareVersions 按数字段比较 x.y.z，预发布后缀（-rc1）低于同号正式版；无法解析时返回 ok=false。
func compareVersions(a, b string) (int, bool) {
	parse := func(v string) ([]int, bool, bool) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		main, pre, _ := strings.Cut(v, "-")
		var parts []int
		for _, p := range strings.Split(main, ".") {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, false, false
			}
			parts = append(parts, n)
		}
		return parts, pre != "", true
	}
	pa, preA, okA := parse(a)
	pb, preB, okB := parse(b)
	if !okA || !okB {
		return 0, false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case preA && !preB:
		return -1, true
	case !preA && preB:
		return 1, true
	}
	return 0, true
}

func (u *selfUpdater) check(proxy string) error {
	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return fmt.Errorf("正在检查更新")
	}
	u.checking = true
	u.mu.Unlock()

	release, err := u.fetchLatest(proxy)

	u.mu.Lock()
	defer u.mu.Unlock()
	u.checking = false
	u.checkedAt = time.Now()
	if err != nil {
		u.lastErr = err.Error()
		return err
	}
	u.lastErr = ""
	if len(release.Body) > selfUpdateNotesMax {
		release.Body = release.Body[:selfUpdateNotesMax] + "\n…"
	}
	if u.hasUpdate(release) && (u.latest == nil || u.latest.TagName != release.TagName) {
		logger.Printf("检查更新：发现新版本 %s（当前 v%s）", release.TagName, u.current)
	}
	u.latest = release
	return nil
}

func (u *selfUpdater) fetchLatest(proxy string) (*selfUpdateRelease, error) {
	url := u.latestAPI
	if proxy != "" {
		url = proxy + url
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 GitHub，请检查网络或配置 GitHub 代理")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GitHub API 请求被限制，请稍后重试或配置代理")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回 HTTP %d", resp.StatusCode)
	}
	var release selfUpdateRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析发布信息失败")
	}
	if _, ok := compareVersions(release.TagName, "0"); !ok {
		return nil, fmt.Errorf("发布标签 %q 不是有效版本号", release.TagName)
	}
	return &release, nil
}

// hasUpdate 调用方持有 u.mu。当前版本无法解析（本地开发构建）时，只要标签不同即提示。
func (u *selfUpdater) hasUpdate(release *selfUpdateRelease) bool {
	if release == nil {
		return false
	}
	cmp, ok := compareVersions(u.current, release.TagName)
	if !ok {
		return strings.TrimPrefix(release.TagName, "v") != u.current
	}
	return cmp < 0
}

func (u *selfUpdater) run(enabled func() bool, proxy func() string) {
	u.mu.Lock()
	if u.stop != nil {
		u.mu.Unlock()
		return
	}
	u.stop, u.done = make(chan struct{}), make(chan struct{})
	stop, done := u.stop, u.done
	u.mu.Unlock()
	go func() {
		defer close(done)
		timer := time.NewTimer(selfUpdateFirstWait)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
			}
			if enabled() {
				if err := u.check(proxy()); err != nil {
					logger.Printf("自动检查更新失败: %v", err)
				}
			}
			timer.Reset(selfUpdateInterval)
		}
	}()
}

func (u *selfUpdater) shutdown() {
	u.mu.Lock()
	stop, done := u.stop, u.done
	u.stop, u.done = nil, nil
	u.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

// installBlocked 返回不能在界面内直接更新的原因；空字符串表示可以。
func (s *Server) installBlocked() string {
	if s.store.GetSettings().DeploymentRole == "gateway" {
		return "网关模式的管理器由系统安装器部署，请使用 install-gateway.sh 升级"
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "当前系统不支持在线更新"
	}
	if !agentClientAssets[selfUpdateAsset()] {
		return "发布文件不包含当前平台 " + runtime.GOOS + "/" + runtime.GOARCH
	}
	dir := filepath.Dir(s.sbmPath)
	if err := syscall.Access(dir, 2); err != nil { // W_OK
		return "当前用户无权写入 " + dir + "，请手动下载替换"
	}
	return ""
}

func selfUpdateAsset() string { return "sbm-" + runtime.GOOS + "-" + runtime.GOARCH }

func (s *Server) updateStatusData() gin.H {
	u := s.updater
	u.mu.Lock()
	defer u.mu.Unlock()
	data := gin.H{
		"current_version": u.current,
		"auto_check":      !s.store.GetSettings().UpdateCheckDisabled,
		"checking":        u.checking,
		"installing":      u.installing,
		"error":           u.lastErr,
		"available":       u.hasUpdate(u.latest),
		"install_blocked": s.installBlocked(),
	}
	if !u.checkedAt.IsZero() {
		data["checked_at"] = u.checkedAt
	}
	if u.latest != nil {
		data["latest"] = gin.H{
			"version":      strings.TrimPrefix(u.latest.TagName, "v"),
			"name":         u.latest.Name,
			"notes":        u.latest.Body,
			"url":          u.latest.HTMLURL,
			"published_at": u.latest.PublishedAt,
		}
	}
	return data
}

func (s *Server) setupUpdateRoutes(r *gin.RouterGroup) {
	r.GET("/update/status", s.getUpdateStatus)
	r.POST("/update/check", s.checkUpdate)
	r.PUT("/update/settings", s.updateUpdateSettings)
	r.POST("/update/install", s.installUpdate)
}

func (s *Server) getUpdateStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": s.updateStatusData()})
}

func (s *Server) checkUpdate(c *gin.Context) {
	if err := s.updater.check(s.store.GetSettings().GithubProxy); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "data": s.updateStatusData()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s.updateStatusData()})
}

func (s *Server) updateUpdateSettings(c *gin.Context) {
	var req struct {
		AutoCheck *bool `json:"auto_check"`
	}
	if c.ShouldBindJSON(&req) != nil || req.AutoCheck == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 auto_check"})
		return
	}
	settings := s.store.GetSettings()
	enabling := settings.UpdateCheckDisabled && *req.AutoCheck
	settings.UpdateCheckDisabled = !*req.AutoCheck
	if err := s.store.UpdateSettings(settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if enabling {
		go func() {
			if err := s.updater.check(settings.GithubProxy); err != nil {
				logger.Printf("检查更新失败: %v", err)
			}
		}()
	}
	c.JSON(http.StatusOK, gin.H{"data": s.updateStatusData()})
}

// installUpdate 下载最新发布中的本平台 sbm，校验后替换当前可执行文件并原地重启。
// 中间件已持有 writeMu，且 Agent 确认窗口内会被拒绝。
func (s *Server) installUpdate(c *gin.Context) {
	var req struct {
		Version string `json:"version"`
	}
	_ = c.ShouldBindJSON(&req)
	if reason := s.installBlocked(); reason != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": reason})
		return
	}
	u := s.updater
	u.mu.Lock()
	latest := u.latest
	if latest == nil || !u.hasUpdate(latest) {
		u.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "没有可安装的新版本，请先检查更新"})
		return
	}
	// 界面展示的版本与服务端记录不一致时拒绝，避免装上用户未确认的版本。
	if req.Version != "" && strings.TrimPrefix(req.Version, "v") != strings.TrimPrefix(latest.TagName, "v") {
		u.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "最新版本已变化，请刷新后重试"})
		return
	}
	if u.installing {
		u.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "正在安装更新"})
		return
	}
	u.installing = true
	u.mu.Unlock()

	proxy := s.store.GetSettings().GithubProxy
	get := func(url string) (*http.Response, error) { return getRelease(u.client, proxy, url) }
	err := fetchReleaseAsset(get, u.release+"/"+latest.TagName+"/", selfUpdateAsset(), s.sbmPath, 0755)
	if err != nil {
		u.mu.Lock()
		u.installing = false
		u.mu.Unlock()
		logger.Printf("下载更新 %s 失败: %v", latest.TagName, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("下载 %s 失败：%v", latest.TagName, err)})
		return
	}
	logger.Printf("已安装 %s 至 %s，正在重启管理器", latest.TagName, s.sbmPath)
	c.JSON(http.StatusOK, gin.H{"message": "更新已安装，管理器正在重启", "data": gin.H{"version": strings.TrimPrefix(latest.TagName, "v")}})
	go func() {
		// 等响应写出且中间件释放 writeMu 后再重启。
		time.Sleep(500 * time.Millisecond)
		s.restartSelf()
	}()
}

// restartSelf 以相同参数 exec 新的二进制：PID 不变，systemd/launchd 与前台运行都适用。
// 实例锁、监听端口等描述符带 CLOEXEC，exec 后由新进程重新获取；受管 sing-box 保持运行并在启动时恢复。
// 持有两把写锁再 exec，确保不会截断进行中的配置写入或运行态操作。
func (s *Server) restartSelf() {
	s.writeMu.Lock()
	s.operationMu.Lock()
	err := s.execSelf(s.sbmPath, os.Args, os.Environ())
	// exec 成功不会返回；失败时继续以旧版本服务，新版本在下次启动后生效。
	logger.Printf("重启管理器失败，新版本将在下次启动后生效: %v", err)
	s.operationMu.Unlock()
	s.writeMu.Unlock()
	s.updater.mu.Lock()
	s.updater.installing = false
	s.updater.mu.Unlock()
}
