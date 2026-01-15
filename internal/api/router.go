package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xiaobei/singbox-manager/internal/builder"
	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/kernel"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"github.com/xiaobei/singbox-manager/internal/parser"
	"github.com/xiaobei/singbox-manager/internal/service"
	"github.com/xiaobei/singbox-manager/internal/storage"
	"github.com/xiaobei/singbox-manager/pkg/procmon"
	"github.com/xiaobei/singbox-manager/pkg/utils"
	"github.com/xiaobei/singbox-manager/web"
)

// generateRandomSecret 生成随机密钥
func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// 如果加密随机数生成失败，返回空字符串
		return ""
	}
	return hex.EncodeToString(bytes)[:length]
}

// Server API 服务器
type Server struct {
	store          *storage.JSONStore
	subService     *service.SubscriptionService
	processManager *daemon.ProcessManager
	launchdManager *daemon.LaunchdManager
	systemdManager *daemon.SystemdManager
	kernelManager  *kernel.Manager
	scheduler      *service.Scheduler
	healthChecker  *daemon.HealthChecker
	router         *gin.Engine
	sbmPath        string // sbm 可执行文件路径
	port           int    // Web 服务端口
	version        string // sbm 版本号

	// 异步配置应用
	configQueue    chan struct{}
	configDone     chan struct{}
	configPending  atomic.Bool // 标记是否有待处理的配置更新
	lastApplyError error
	lastApplyMu    sync.RWMutex
}

// NewServer 创建 API 服务器
func NewServer(store *storage.JSONStore, processManager *daemon.ProcessManager, launchdManager *daemon.LaunchdManager, systemdManager *daemon.SystemdManager, sbmPath string, port int, version string) *Server {
	gin.SetMode(gin.ReleaseMode)

	subService := service.NewSubscriptionService(store)

	// 创建内核管理器
	kernelManager := kernel.NewManager(store.GetDataDir(), store.GetSettings)

	// 创建健康检查器
	healthChecker := daemon.NewHealthChecker(processManager)

	s := &Server{
		store:          store,
		subService:     subService,
		processManager: processManager,
		launchdManager: launchdManager,
		systemdManager: systemdManager,
		kernelManager:  kernelManager,
		scheduler:      service.NewScheduler(store, subService),
		healthChecker:  healthChecker,
		router:         gin.Default(),
		sbmPath:        sbmPath,
		port:           port,
		version:        version,
		configQueue:    make(chan struct{}, 1), // 缓冲区为 1 实现去重
		configDone:     make(chan struct{}),
	}

	// 设置调度器的更新回调
	s.scheduler.SetUpdateCallback(s.autoApplyConfig)

	// 启动异步配置应用 worker
	go s.configApplyWorker()

	// 配置并启动健康检查器
	settings := store.GetSettings()
	healthChecker.Configure(
		settings.HealthCheckEnabled,
		settings.HealthCheckInterval,
		settings.AutoRestart,
		settings.ClashAPIPort,
		settings.ClashAPISecret,
	)
	healthChecker.Start()

	s.setupRoutes()
	return s
}

// StartScheduler 启动定时任务调度器
func (s *Server) StartScheduler() {
	s.scheduler.Start()
}

// StopScheduler 停止定时任务调度器
func (s *Server) StopScheduler() {
	s.scheduler.Stop()
}

// setupRoutes 设置路由
func (s *Server) setupRoutes() {
	// CORS 配置
	s.router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// API 路由组
	api := s.router.Group("/api")
	{
		// 订阅管理
		api.GET("/subscriptions", s.getSubscriptions)
		api.POST("/subscriptions", s.addSubscription)
		api.PUT("/subscriptions/:id", s.updateSubscription)
		api.DELETE("/subscriptions/:id", s.deleteSubscription)
		api.POST("/subscriptions/:id/refresh", s.refreshSubscription)
		api.POST("/subscriptions/refresh-all", s.refreshAllSubscriptions)
		api.POST("/subscriptions/:id/nodes/:nodeIndex/toggle", s.toggleNodeDisabled)

		// 过滤器管理
		api.GET("/filters", s.getFilters)
		api.POST("/filters", s.addFilter)
		api.PUT("/filters/:id", s.updateFilter)
		api.DELETE("/filters/:id", s.deleteFilter)

		// 规则管理
		api.GET("/rules", s.getRules)
		api.POST("/rules", s.addRule)
		api.PUT("/rules/:id", s.updateRule)
		api.DELETE("/rules/:id", s.deleteRule)

		// 规则组管理
		api.GET("/rule-groups", s.getRuleGroups)
		api.PUT("/rule-groups/:id", s.updateRuleGroup)

		// 规则集验证
		api.GET("/ruleset/validate", s.validateRuleSet)

		// 设置
		api.GET("/settings", s.getSettings)
		api.PUT("/settings", s.updateSettings)

		// 系统 hosts
		api.GET("/system-hosts", s.getSystemHosts)

		// 配置生成
		api.POST("/config/generate", s.generateConfig)
		api.POST("/config/apply", s.applyConfig)
		api.GET("/config/preview", s.previewConfig)

		// 服务管理
		api.GET("/service/status", s.getServiceStatus)
		api.POST("/service/start", s.startService)
		api.POST("/service/stop", s.stopService)
		api.POST("/service/restart", s.restartService)
		api.POST("/service/reload", s.reloadService)

		// launchd 管理
		api.GET("/launchd/status", s.getLaunchdStatus)
		api.POST("/launchd/install", s.installLaunchd)
		api.POST("/launchd/uninstall", s.uninstallLaunchd)
		api.POST("/launchd/restart", s.restartLaunchd)

		// systemd 管理
		api.GET("/systemd/status", s.getSystemdStatus)
		api.POST("/systemd/install", s.installSystemd)
		api.POST("/systemd/uninstall", s.uninstallSystemd)
		api.POST("/systemd/restart", s.restartSystemd)

		// 统一守护进程管理（自动判断系统）
		api.GET("/daemon/status", s.getDaemonStatus)
		api.POST("/daemon/install", s.installDaemon)
		api.POST("/daemon/uninstall", s.uninstallDaemon)
		api.POST("/daemon/restart", s.restartDaemon)

		// 系统监控
		api.GET("/monitor/system", s.getSystemInfo)
		api.GET("/monitor/logs", s.getLogs)
		api.GET("/monitor/logs/sbm", s.getAppLogs)
		api.GET("/monitor/logs/singbox", s.getSingboxLogs)

		// 节点
		api.GET("/nodes", s.getAllNodes)
		api.GET("/nodes/countries", s.getCountryGroups)
		api.GET("/nodes/country/:code", s.getNodesByCountry)
		api.POST("/nodes/parse", s.parseNodeURL)

		// 手动节点
		api.GET("/manual-nodes", s.getManualNodes)
		api.POST("/manual-nodes", s.addManualNode)
		api.PUT("/manual-nodes/:id", s.updateManualNode)
		api.DELETE("/manual-nodes/:id", s.deleteManualNode)

		// 内核管理
		api.GET("/kernel/info", s.getKernelInfo)
		api.GET("/kernel/releases", s.getKernelReleases)
		api.POST("/kernel/download", s.startKernelDownload)
		api.GET("/kernel/progress", s.getKernelProgress)
	}

	// 静态文件服务（前端，使用嵌入的文件系统）
	distFS, err := web.GetDistFS()
	if err != nil {
		logger.Printf("加载前端资源失败: %v", err)
	} else {
		// 获取 assets 子目录
		assetsFS, _ := fs.Sub(distFS, "assets")
		s.router.StaticFS("/assets", http.FS(assetsFS))

		// 处理根路径和所有未匹配的路由（SPA 支持）
		indexHTML, _ := fs.ReadFile(distFS, "index.html")
		s.router.GET("/", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
		})
		s.router.NoRoute(func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
		})
	}
}

// Run 运行服务器
func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

// RunServer 返回 http.Server 用于优雅退出
func (s *Server) RunServer(addr string) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: s.router,
	}
}

// ==================== 订阅 API ====================

func (s *Server) getSubscriptions(c *gin.Context) {
	subs := s.subService.GetAll()
	c.JSON(http.StatusOK, gin.H{"data": subs})
}

func (s *Server) addSubscription(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
		URL  string `json:"url" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sub, err := s.subService.Add(req.Name, req.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"data": sub, "warning": "添加成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": sub})
}

func (s *Server) updateSubscription(c *gin.Context) {
	id := c.Param("id")

	var sub storage.Subscription
	if err := c.ShouldBindJSON(&sub); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sub.ID = id
	if err := s.subService.Update(sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "更新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (s *Server) deleteSubscription(c *gin.Context) {
	id := c.Param("id")

	if err := s.subService.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "删除成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

func (s *Server) refreshSubscription(c *gin.Context) {
	id := c.Param("id")

	if err := s.subService.Refresh(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "刷新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "刷新成功"})
}

func (s *Server) refreshAllSubscriptions(c *gin.Context) {
	if err := s.subService.RefreshAll(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "刷新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "刷新成功"})
}

// toggleNodeDisabled 切换节点禁用状态
func (s *Server) toggleNodeDisabled(c *gin.Context) {
	subID := c.Param("id")
	nodeIndexStr := c.Param("nodeIndex")

	nodeIndex, err := strconv.Atoi(nodeIndexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的节点索引"})
		return
	}

	sub := s.store.GetSubscription(subID)
	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "订阅不存在"})
		return
	}

	if nodeIndex < 0 || nodeIndex >= len(sub.Nodes) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "节点索引越界"})
		return
	}

	// 切换禁用状态
	sub.Nodes[nodeIndex].Disabled = !sub.Nodes[nodeIndex].Disabled

	// 保存更新后的节点列表
	if err := s.store.SaveSubscriptionNodes(subID, sub.Nodes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "已更新",
		"disabled": sub.Nodes[nodeIndex].Disabled,
	})
}

// ==================== 过滤器 API ====================

func (s *Server) getFilters(c *gin.Context) {
	filters := s.store.GetFilters()
	c.JSON(http.StatusOK, gin.H{"data": filters})
}

func (s *Server) addFilter(c *gin.Context) {
	var filter storage.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 生成 ID
	filter.ID = uuid.New().String()

	if err := s.store.AddFilter(filter); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"data": filter, "warning": "添加成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": filter})
}

func (s *Server) updateFilter(c *gin.Context) {
	id := c.Param("id")

	var filter storage.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter.ID = id
	if err := s.store.UpdateFilter(filter); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "更新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (s *Server) deleteFilter(c *gin.Context) {
	id := c.Param("id")

	if err := s.store.DeleteFilter(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "删除成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ==================== 规则 API ====================

func (s *Server) getRules(c *gin.Context) {
	rules := s.store.GetRules()
	c.JSON(http.StatusOK, gin.H{"data": rules})
}

func (s *Server) addRule(c *gin.Context) {
	var rule storage.Rule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 生成 ID
	rule.ID = uuid.New().String()

	if err := s.store.AddRule(rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"data": rule, "warning": "添加成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": rule})
}

func (s *Server) updateRule(c *gin.Context) {
	id := c.Param("id")

	var rule storage.Rule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rule.ID = id
	if err := s.store.UpdateRule(rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "更新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (s *Server) deleteRule(c *gin.Context) {
	id := c.Param("id")

	if err := s.store.DeleteRule(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "删除成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ==================== 规则组 API ====================

func (s *Server) getRuleGroups(c *gin.Context) {
	ruleGroups := s.store.GetRuleGroups()
	c.JSON(http.StatusOK, gin.H{"data": ruleGroups})
}

func (s *Server) updateRuleGroup(c *gin.Context) {
	id := c.Param("id")

	var ruleGroup storage.RuleGroup
	if err := c.ShouldBindJSON(&ruleGroup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ruleGroup.ID = id
	if err := s.store.UpdateRuleGroup(ruleGroup); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "更新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

// ==================== 规则集验证 API ====================

func (s *Server) validateRuleSet(c *gin.Context) {
	ruleType := c.Query("type") // geosite 或 geoip
	name := c.Query("name")     // 规则集名称

	if ruleType == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数 type 和 name 是必需的"})
		return
	}

	if ruleType != "geosite" && ruleType != "geoip" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type 必须是 geosite 或 geoip"})
		return
	}

	settings := s.store.GetSettings()
	var url string
	var tag string

	if ruleType == "geosite" {
		tag = "geosite-" + name
		url = settings.RuleSetBaseURL + "/geosite-" + name + ".srs"
	} else {
		tag = "geoip-" + name
		// geoip 使用相对路径
		url = settings.RuleSetBaseURL + "/../rule-set-geoip/geoip-" + name + ".srs"
	}

	// 如果配置了 GitHub 代理，添加代理前缀
	if settings.GithubProxy != "" {
		url = settings.GithubProxy + url
	}

	// 发送 HEAD 请求检查文件是否存在
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: utils.GetHTTPClient().Transport,
	}

	resp, err := client.Head(url)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"valid":   false,
			"url":     url,
			"tag":     tag,
			"message": "无法访问规则集: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{
			"valid":   true,
			"url":     url,
			"tag":     tag,
			"message": "规则集存在",
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"valid":   false,
			"url":     url,
			"tag":     tag,
			"message": "规则集不存在 (HTTP " + strconv.Itoa(resp.StatusCode) + ")",
		})
	}
}

// ==================== 设置 API ====================

func (s *Server) getSettings(c *gin.Context) {
	settings := s.store.GetSettings()
	// 不再覆盖 WebPort，使用 data.json 中存储的值
	// settings.WebPort = s.port
	c.JSON(http.StatusOK, gin.H{"data": settings})
}

func (s *Server) updateSettings(c *gin.Context) {
	var settings storage.Settings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 根据局域网访问设置处理 secret
	if settings.AllowLAN {
		// 开启局域网访问且 secret 为空时，自动生成一个
		if settings.ClashAPISecret == "" {
			settings.ClashAPISecret = generateRandomSecret(16)
		}
	} else {
		// 关闭局域网访问时，清除 secret
		settings.ClashAPISecret = ""
	}

	if err := s.store.UpdateSettings(&settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新进程管理器的配置路径（sing-box 路径是固定的，无需更新）
	s.processManager.SetConfigPath(s.resolvePath(settings.ConfigPath))

	// 重启调度器（可能更新了定时间隔）
	s.scheduler.Restart()

	// 更新健康检查器配置
	s.healthChecker.Stop()
	s.healthChecker.Configure(
		settings.HealthCheckEnabled,
		settings.HealthCheckInterval,
		settings.AutoRestart,
		settings.ClashAPIPort,
		settings.ClashAPISecret,
	)
	s.healthChecker.Start()

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"data": settings, "warning": "更新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": settings, "message": "更新成功"})
}

// ==================== 系统 hosts API ====================

func (s *Server) getSystemHosts(c *gin.Context) {
	hosts := builder.ParseSystemHosts()

	var entries []storage.HostEntry
	for domain, ips := range hosts {
		entries = append(entries, storage.HostEntry{
			ID:      "system-" + domain,
			Domain:  domain,
			IPs:     ips,
			Enabled: true,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": entries})
}

// ==================== 配置 API ====================

func (s *Server) generateConfig(c *gin.Context) {
	configJSON, err := s.buildConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": configJSON})
}

func (s *Server) previewConfig(c *gin.Context) {
	configJSON, err := s.buildConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.String(http.StatusOK, configJSON)
}

func (s *Server) applyConfig(c *gin.Context) {
	configJSON, err := s.buildConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 读取旧配置（用于判断是否需要硬重启）
	settings := s.store.GetSettings()
	configPath := s.resolvePath(settings.ConfigPath)
	oldConfig, _ := os.ReadFile(configPath)

	// 保存新配置文件
	if err := s.saveConfigFile(configPath, configJSON); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 检查配置
	if err := s.processManager.Check(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 重启服务
	if s.processManager.IsRunning() {
		// 判断是否需要硬重启
		needHardRestart := s.needHardRestart(oldConfig, []byte(configJSON))
		
		if needHardRestart {
			logger.Printf("配置变更需要硬重启")
			if err := s.processManager.Restart(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "配置已应用（硬重启）"})
		} else {
			logger.Printf("配置变更使用软重载")
			if err := s.processManager.Reload(); err != nil {
				// 软重载失败，尝试硬重启
				logger.Printf("软重载失败，尝试硬重启: %v", err)
				if err := s.processManager.Restart(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"message": "配置已应用（软重载失败，已硬重启）"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "配置已应用（软重载）"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "配置已保存"})
}

// needHardRestart 判断配置变更是否需要硬重启
// 需要硬重启的情况：inbounds、experimental、log 配置变化
func (s *Server) needHardRestart(oldConfig, newConfig []byte) bool {
	if len(oldConfig) == 0 {
		return true // 没有旧配置，需要硬重启
	}

	var oldCfg, newCfg map[string]interface{}
	if err := json.Unmarshal(oldConfig, &oldCfg); err != nil {
		return true
	}
	if err := json.Unmarshal(newConfig, &newCfg); err != nil {
		return true
	}

	// 检查需要硬重启的字段
	hardRestartFields := []string{"inbounds", "experimental", "log"}
	for _, field := range hardRestartFields {
		oldVal, _ := json.Marshal(oldCfg[field])
		newVal, _ := json.Marshal(newCfg[field])
		if string(oldVal) != string(newVal) {
			logger.Printf("字段 %s 变更，需要硬重启", field)
			return true
		}
	}

	return false
}

func (s *Server) buildConfig() (string, error) {
	settings := s.store.GetSettings()
	nodes := s.store.GetAllNodesPtr()
	filters := s.store.GetFilters()
	rules := s.store.GetRules()
	ruleGroups := s.store.GetRuleGroups()

	// 创建规则集服务并确保所有需要的规则集已下载
	ruleSetService := service.NewRuleSetService(s.store, s.store.GetDataDir())
	missing, err := ruleSetService.EnsureRuleSets(ruleGroups, rules)
	if err != nil {
		logger.Printf("规则集下载警告: %v (缺失: %v)", err, missing)
		// 不中断，继续生成配置（使用本地已有的规则集）
	}

	// 使用本地规则集构建配置
	b := builder.NewConfigBuilder(settings, nodes, filters, rules, ruleGroups).
		WithLocalRuleSet(ruleSetService.GetRuleSetDir())
	return b.BuildJSON()
}

func (s *Server) saveConfigFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// resolvePath 将相对路径解析为基于数据目录的绝对路径
func (s *Server) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(s.store.GetDataDir(), path)
}

// autoApplyConfig 自动应用配置（异步，非阻塞）
// 返回上次异步应用的错误（如果有），便于调用者感知历史失败
func (s *Server) autoApplyConfig() error {
	// 获取并清除上次的错误
	s.lastApplyMu.Lock()
	lastErr := s.lastApplyError
	s.lastApplyError = nil
	s.lastApplyMu.Unlock()

	settings := s.store.GetSettings()
	if !settings.AutoApply {
		return lastErr
	}

	// 设置待处理标记，确保最新变更会被应用
	s.configPending.Store(true)

	// 非阻塞发送配置应用信号
	select {
	case s.configQueue <- struct{}{}:
		// 成功发送信号，唤醒 worker
	default:
		// 队列已满，worker 正在处理中
		// configPending 标记会确保 worker 处理完后再次检查
	}

	return lastErr
}

// configApplyWorker 后台配置应用 worker
func (s *Server) configApplyWorker() {
	for {
		select {
		case <-s.configQueue:
			// 循环处理，直到没有待处理的更新
			for s.configPending.Swap(false) {
				s.doApplyConfig()
			}
		case <-s.configDone:
			// 收到停止信号
			return
		}
	}
}

// doApplyConfig 实际执行配置应用
func (s *Server) doApplyConfig() {
	settings := s.store.GetSettings()

	// 生成配置
	configJSON, err := s.buildConfig()
	if err != nil {
		logger.Printf("生成配置失败: %v", err)
		s.setLastApplyError(err)
		return
	}

	// 保存配置文件
	if err := s.saveConfigFile(s.resolvePath(settings.ConfigPath), configJSON); err != nil {
		logger.Printf("保存配置失败: %v", err)
		s.setLastApplyError(err)
		return
	}

	// 如果 sing-box 正在运行，则重启
	if s.processManager.IsRunning() {
		if err := s.processManager.Restart(); err != nil {
			logger.Printf("重启 sing-box 失败: %v", err)
			s.setLastApplyError(err)
			return
		}
	}

	// 成功时清除错误
	s.setLastApplyError(nil)
}

// setLastApplyError 设置最后一次应用错误
func (s *Server) setLastApplyError(err error) {
	s.lastApplyMu.Lock()
	s.lastApplyError = err
	s.lastApplyMu.Unlock()
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	close(s.configDone)
	s.scheduler.Stop()
}

// ==================== 服务 API ====================

func (s *Server) getServiceStatus(c *gin.Context) {
	running := s.processManager.IsRunning()
	pid := s.processManager.GetPID()

	version := ""
	if v, err := s.processManager.Version(); err == nil {
		version = v
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"running":     running,
			"pid":         pid,
			"version":     version,
			"sbm_version": s.version,
		},
	})
}

func (s *Server) startService(c *gin.Context) {
	if err := s.processManager.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已启动"})
}

func (s *Server) stopService(c *gin.Context) {
	if err := s.processManager.Stop(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已停止"})
}

func (s *Server) restartService(c *gin.Context) {
	if err := s.processManager.Restart(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已重启"})
}

func (s *Server) reloadService(c *gin.Context) {
	if err := s.processManager.Reload(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "配置已重载"})
}

// ==================== launchd API ====================

func (s *Server) getLaunchdStatus(c *gin.Context) {
	if s.launchdManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"installed": false,
				"running":   false,
				"plistPath": "",
				"supported": false,
			},
		})
		return
	}

	installed := s.launchdManager.IsInstalled()
	running := s.launchdManager.IsRunning()

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"installed": installed,
			"running":   running,
			"plistPath": s.launchdManager.GetPlistPath(),
			"supported": true,
		},
	})
}

func (s *Server) installLaunchd(c *gin.Context) {
	if s.launchdManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持 launchd 服务"})
		return
	}

	// 获取用户主目录（支持多种方式）
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		// 备用方案：使用 os/user 包
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			homeDir = u.HomeDir
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户目录失败"})
			return
		}
	}

	// 确保日志目录存在
	logsDir := s.store.GetDataDir() + "/logs"

	config := daemon.LaunchdConfig{
		SbmPath:    s.sbmPath,
		DataDir:    s.store.GetDataDir(),
		Port:       strconv.Itoa(s.port),
		LogPath:    logsDir,
		WorkingDir: s.store.GetDataDir(),
		HomeDir:    homeDir,
		RunAtLoad:  true,
		KeepAlive:  true,
	}

	if err := s.launchdManager.Install(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 安装成功后启动服务
	if err := s.launchdManager.Start(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "服务已安装，但启动失败: " + err.Error() + "。请重启电脑或手动执行 launchctl load 命令",
			"action":  "manual",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "服务已安装并启动，您可以关闭此终端窗口。sbm 将在后台运行并开机自启。",
		"action":  "exit",
	})
}

func (s *Server) uninstallLaunchd(c *gin.Context) {
	if s.launchdManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持 launchd 服务"})
		return
	}

	if err := s.launchdManager.Uninstall(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已卸载"})
}

func (s *Server) restartLaunchd(c *gin.Context) {
	if s.launchdManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持 launchd 服务"})
		return
	}

	if err := s.launchdManager.Restart(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已重启"})
}

// ==================== systemd API ====================

func (s *Server) getSystemdStatus(c *gin.Context) {
	if s.systemdManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"installed":   false,
				"running":     false,
				"servicePath": "",
				"supported":   false,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"installed":   s.systemdManager.IsInstalled(),
			"running":     s.systemdManager.IsRunning(),
			"servicePath": s.systemdManager.GetServicePath(),
			"supported":   true,
		},
	})
}

func (s *Server) installSystemd(c *gin.Context) {
	if s.systemdManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持 systemd 服务"})
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			homeDir = u.HomeDir
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户目录失败"})
			return
		}
	}

	logsDir := s.store.GetDataDir() + "/logs"

	config := daemon.SystemdConfig{
		SbmPath:    s.sbmPath,
		DataDir:    s.store.GetDataDir(),
		Port:       strconv.Itoa(s.port),
		LogPath:    logsDir,
		WorkingDir: s.store.GetDataDir(),
		HomeDir:    homeDir,
		RunAtLoad:  true,
		KeepAlive:  true,
	}

	if err := s.systemdManager.Install(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := s.systemdManager.Start(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "服务已安装，但启动失败: " + err.Error() + "。请执行 systemctl --user start singbox-manager",
			"action":  "manual",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "服务已安装并启动，您可以关闭此终端窗口。sbm 将在后台运行并开机自启。",
		"action":  "exit",
	})
}

func (s *Server) uninstallSystemd(c *gin.Context) {
	if s.systemdManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持 systemd 服务"})
		return
	}

	if err := s.systemdManager.Uninstall(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已卸载"})
}

func (s *Server) restartSystemd(c *gin.Context) {
	if s.systemdManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持 systemd 服务"})
		return
	}

	if err := s.systemdManager.Restart(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已重启"})
}

// ==================== 统一守护进程 API ====================

func (s *Server) getDaemonStatus(c *gin.Context) {
	platform := runtime.GOOS
	var installed, running, supported bool
	var configPath string

	switch platform {
	case "darwin":
		if s.launchdManager != nil {
			supported = true
			installed = s.launchdManager.IsInstalled()
			running = s.launchdManager.IsRunning()
			configPath = s.launchdManager.GetPlistPath()
		}
	case "linux":
		if s.systemdManager != nil {
			supported = true
			installed = s.systemdManager.IsInstalled()
			running = s.systemdManager.IsRunning()
			configPath = s.systemdManager.GetServicePath()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"installed":  installed,
			"running":    running,
			"configPath": configPath,
			"supported":  supported,
			"platform":   platform,
		},
	})
}

func (s *Server) installDaemon(c *gin.Context) {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			homeDir = u.HomeDir
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户目录失败"})
			return
		}
	}

	logsDir := s.store.GetDataDir() + "/logs"

	switch runtime.GOOS {
	case "darwin":
		if s.launchdManager == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
			return
		}
		config := daemon.LaunchdConfig{
			SbmPath:    s.sbmPath,
			DataDir:    s.store.GetDataDir(),
			Port:       strconv.Itoa(s.port),
			LogPath:    logsDir,
			WorkingDir: s.store.GetDataDir(),
			HomeDir:    homeDir,
			RunAtLoad:  true,
			KeepAlive:  true,
		}
		if err := s.launchdManager.Install(config); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := s.launchdManager.Start(); err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "服务已安装，但启动失败: " + err.Error(), "action": "manual"})
			return
		}
	case "linux":
		if s.systemdManager == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
			return
		}
		config := daemon.SystemdConfig{
			SbmPath:    s.sbmPath,
			DataDir:    s.store.GetDataDir(),
			Port:       strconv.Itoa(s.port),
			LogPath:    logsDir,
			WorkingDir: s.store.GetDataDir(),
			HomeDir:    homeDir,
			RunAtLoad:  true,
			KeepAlive:  true,
		}
		if err := s.systemdManager.Install(config); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := s.systemdManager.Start(); err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "服务已安装，但启动失败: " + err.Error(), "action": "manual"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "服务已安装并启动", "action": "exit"})
}

func (s *Server) uninstallDaemon(c *gin.Context) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		if s.launchdManager == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
			return
		}
		err = s.launchdManager.Uninstall()
	case "linux":
		if s.systemdManager == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
			return
		}
		err = s.systemdManager.Uninstall()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已卸载"})
}

func (s *Server) restartDaemon(c *gin.Context) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		if s.launchdManager == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
			return
		}
		err = s.launchdManager.Restart()
	case "linux":
		if s.systemdManager == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
			return
		}
		err = s.systemdManager.Restart()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前系统不支持守护进程服务"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "服务已重启"})
}

// ==================== 监控 API ====================

// ProcessStats 进程资源统计
type ProcessStats struct {
	PID        int     `json:"pid"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   float64 `json:"memory_mb"`
}

// cachedSystemInfo 缓存的系统信息
type cachedSystemInfo struct {
	data      gin.H
	timestamp time.Time
	mu        sync.RWMutex
}

func (c *cachedSystemInfo) get() (gin.H, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 缓存有效期 2 秒
	if time.Since(c.timestamp) < 2*time.Second && c.data != nil {
		return c.data, true
	}
	return nil, false
}

func (c *cachedSystemInfo) set(data gin.H) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.timestamp = time.Now()
}

var systemInfoCache = &cachedSystemInfo{}

func (s *Server) getSystemInfo(c *gin.Context) {
	// 尝试从缓存获取
	if cached, ok := systemInfoCache.get(); ok {
		c.JSON(http.StatusOK, gin.H{"data": cached})
		return
	}

	result := gin.H{}

	// 获取 sbm 进程信息
	sbmPid := os.Getpid()
	if stats, err := procmon.GetProcessStats(sbmPid); err == nil {
		result["sbm"] = ProcessStats{
			PID:        stats.PID,
			CPUPercent: stats.CPUPercent,
			MemoryMB:   stats.MemoryMB,
		}
	}

	// 获取 sing-box 进程信息
	if s.processManager.IsRunning() {
		singboxPid := s.processManager.GetPID()
		if stats, err := procmon.GetProcessStats(singboxPid); err == nil {
			result["singbox"] = ProcessStats{
				PID:        stats.PID,
				CPUPercent: stats.CPUPercent,
				MemoryMB:   stats.MemoryMB,
			}
		}
	}

	// 更新缓存
	systemInfoCache.set(result)

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (s *Server) getLogs(c *gin.Context) {
	lines := 200 // 默认返回 200 行
	if linesParam := c.Query("lines"); linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			lines = n
		}
	}

	// 返回程序日志，不混合 sing-box 输出
	logs, err := logger.ReadAppLogs(lines)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// getAppLogs 获取应用日志
func (s *Server) getAppLogs(c *gin.Context) {
	lines := 200 // 默认返回 200 行
	if linesParam := c.Query("lines"); linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			lines = n
		}
	}

	logs, err := logger.ReadAppLogs(lines)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// getSingboxLogs 获取 sing-box 日志
func (s *Server) getSingboxLogs(c *gin.Context) {
	lines := 200 // 默认返回 200 行
	if linesParam := c.Query("lines"); linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			lines = n
		}
	}

	logs, err := logger.ReadSingboxLogs(lines)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// ==================== 节点 API ====================

func (s *Server) getAllNodes(c *gin.Context) {
	nodes := s.store.GetAllNodes()
	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

func (s *Server) getCountryGroups(c *gin.Context) {
	groups := s.store.GetCountryGroups()
	c.JSON(http.StatusOK, gin.H{"data": groups})
}

func (s *Server) getNodesByCountry(c *gin.Context) {
	code := c.Param("code")
	nodes := s.store.GetNodesByCountry(code)
	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

func (s *Server) parseNodeURL(c *gin.Context) {
	var req struct {
		URL string `json:"url" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node, err := parser.ParseURL(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解析失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": node})
}

// ==================== 手动节点 API ====================

func (s *Server) getManualNodes(c *gin.Context) {
	nodes := s.store.GetManualNodes()
	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

func (s *Server) addManualNode(c *gin.Context) {
	var node storage.ManualNode
	if err := c.ShouldBindJSON(&node); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 生成 ID
	node.ID = uuid.New().String()

	if err := s.store.AddManualNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"data": node, "warning": "添加成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": node})
}

func (s *Server) updateManualNode(c *gin.Context) {
	id := c.Param("id")

	var node storage.ManualNode
	if err := c.ShouldBindJSON(&node); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node.ID = id
	if err := s.store.UpdateManualNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "更新成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (s *Server) deleteManualNode(c *gin.Context) {
	id := c.Param("id")

	if err := s.store.DeleteManualNode(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 自动应用配置
	if err := s.autoApplyConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "删除成功，但自动应用配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ==================== 内核管理 API ====================

func (s *Server) getKernelInfo(c *gin.Context) {
	info := s.kernelManager.GetInfo()
	c.JSON(http.StatusOK, gin.H{"data": info})
}

func (s *Server) getKernelReleases(c *gin.Context) {
	releases, err := s.kernelManager.FetchReleases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 只返回版本号和名称，不返回完整的 assets
	type ReleaseInfo struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}

	result := make([]ReleaseInfo, len(releases))
	for i, r := range releases {
		result[i] = ReleaseInfo{
			TagName: r.TagName,
			Name:    r.Name,
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (s *Server) startKernelDownload(c *gin.Context) {
	var req struct {
		Version string `json:"version" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.kernelManager.StartDownload(req.Version); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "下载已开始"})
}

func (s *Server) getKernelProgress(c *gin.Context) {
	progress := s.kernelManager.GetProgress()
	c.JSON(http.StatusOK, gin.H{"data": progress})
}
