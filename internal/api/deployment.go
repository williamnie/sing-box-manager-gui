package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/builder"
	"github.com/xiaobei/singbox-manager/internal/gateway"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

func (s *Server) setupDeploymentRoutes(r *gin.RouterGroup) {
	r.GET("/gateway/status", s.gatewayStatus)
	r.GET("/gateway/clients", s.gatewayClients)
	r.PUT("/gateway/settings", s.saveGatewaySettings)
	r.POST("/gateway/preview", s.gatewayPreview)
	r.POST("/gateway/check", s.gatewayCheck)
	r.POST("/gateway/apply", s.gatewayApply)
	r.POST("/gateway/rollback", s.gatewayRollback)
	r.POST("/gateway/restart-manager", s.restartGatewayManager)
	r.POST("/config/check", s.checkConfig)
	r.GET("/config/diff", s.configDiff)
	r.GET("/config/versions", s.configVersions)
	r.POST("/config/restore", s.restorePreviousConfig)
	r.GET("/rules/overview", s.rulesOverview)
	r.POST("/proxy-plan/preview", s.previewProxyPlan)
	r.PUT("/proxy-plan", s.saveProxyPlan)
	r.PUT("/rules/imported/:index", s.updateImportedRule)
	r.DELETE("/rules/imported/:index", s.deleteImportedRule)
	r.POST("/config/import/preview", s.migrationPreview)
	r.POST("/config/import/apply", s.migrationImport)
	r.POST("/config/import/rollback", s.migrationRollback)
	// 兼容既有客户端；主要界面使用来源无关的配置管理入口。
	r.POST("/migration/preview", s.migrationPreview)
	r.POST("/migration/import", s.migrationImport)
	r.POST("/migration/rollback", s.migrationRollback)
}
func (s *Server) buildSettings(settings *storage.Settings, preview bool) (string, error) {
	data := s.store.Snapshot()
	data.Settings = settings
	return s.buildData(data, preview)
}

func (s *Server) buildData(data *storage.AppData, preview bool) (string, error) {
	return s.dataBuilder(data, preview).BuildJSON()
}

func (s *Server) dataBuilder(data *storage.AppData, preview bool) *builder.ConfigBuilder {
	settings := data.Settings
	var nodes []storage.Node
	for _, sub := range data.Subscriptions {
		if sub.Enabled {
			for _, node := range sub.Nodes {
				node.SubscriptionID = sub.ID
				nodes = append(nodes, node)
			}
		}
	}
	for _, n := range data.ManualNodes {
		if n.Enabled {
			nodes = append(nodes, n.Node)
		}
	}
	b := builder.NewConfigBuilder(settings, nodes, data.Filters, data.Rules, data.RuleGroups).WithPlatform(s.platform)
	if version, err := s.processManager.Version(); err == nil {
		b.WithSingBoxVersion(version)
	} else if preview && settings.DeploymentRole == "gateway" {
		b.WithSingBoxVersion("1.14.1")
	}
	if preview && settings.DeploymentRole == "gateway" {
		b.WithPlatform("linux")
	}
	return b
}
func (s *Server) applyManagedConfig() error {
	settings := s.store.GetSettings()
	if err := s.requireGatewayState(settings); err != nil {
		return err
	}
	candidate, err := s.buildSettings(settings, false)
	if err != nil {
		return err
	}
	return s.processManager.ApplyConfig([]byte(candidate))
}
func (s *Server) saveGatewaySettings(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	var settings storage.Settings
	if c.ShouldBindJSON(&settings) != nil {
		c.JSON(400, gin.H{"error": "设置格式无效"})
		return
	}
	storage.NormalizeSettings(&settings)
	old := s.store.GetSettings()
	settings.ImportedPolicy = old.ImportedPolicy
	settings.ProxyPlan = old.ProxyPlan
	// Enabled 由显式应用/恢复维护，保存草案不能启动系统操作。
	settings.Gateway.Enabled = old.Gateway.Enabled
	if old.Gateway.Enabled && (settings.DeploymentRole != old.DeploymentRole || !reflect.DeepEqual(gateway.Normalize(settings.Gateway), gateway.Normalize(old.Gateway))) {
		c.JSON(409, gin.H{"error": "修改活动网关拓扑前请先恢复网关"})
		return
	}
	if err := storage.ValidateSettings(&settings); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if settings.DeploymentRole == "gateway" {
		if err := gateway.Validate(gateway.Normalize(settings.Gateway)); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
	}
	if err := s.store.UpdateSettings(&settings); err != nil {
		c.JSON(500, gin.H{"error": "保存失败"})
		return
	}
	c.JSON(200, gin.H{"data": settings, "message": "部署草案已保存；检查并应用后才会生效"})
}
func (s *Server) candidateSettings(c *gin.Context) (*storage.Settings, error) {
	settings := s.store.GetSettings()
	if c.Request.ContentLength > 0 {
		var req struct {
			Settings *storage.Settings `json:"settings"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, err
		}
		if req.Settings != nil {
			settings = req.Settings
		}
	}
	storage.NormalizeSettings(settings)
	return settings, nil
}
func (s *Server) gatewayPreview(c *gin.Context) {
	settings, err := s.candidateSettings(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "设置格式无效"})
		return
	}
	candidate, err := s.buildSettings(settings, true)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	plan, planErr := gateway.Preview(settings.DeploymentRole, settings.Gateway)
	warnings := []string{"离线配置预览不验证转发覆盖；设备默认网关和 IPv6 路由必须经过此设备。"}
	if settings.DeploymentRole == "gateway" && gateway.Normalize(settings.Gateway).AccessMode == "dns" {
		warnings = []string{builder.DNSBypassWarnings(settings.Gateway), "终端须使用旁路 DNS，主路由须配置 FakeIP 网段到旁路 LAN 地址的静态路由；终端默认网关保持主路由。更换地址池前须等待/清除客户端 DNS 缓存并同步更新路由。"}
	}
	if planErr != nil {
		warnings = append(warnings, "网关计划验证失败: "+planErr.Error())
	}
	c.JSON(200, gin.H{"data": gin.H{"config": redactJSON(candidate), "plan": plan, "warnings": warnings, "target_version": "1.14 (预览默认；应用使用已安装版本)"}})
}
func (s *Server) gatewayCheck(c *gin.Context) {
	settings, err := s.candidateSettings(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "设置格式无效"})
		return
	}
	if s.platform != "linux" {
		c.JSON(400, gin.H{"error": "本机不是 Linux；可预览，系统检查须在隔离 Linux 环境执行"})
		return
	}
	candidate, err := s.buildSettings(settings, false)
	if err == nil {
		err = s.processManager.CheckConfig([]byte(candidate))
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	result, err := s.gateway.Check(c.Request.Context(), settings.DeploymentRole, settings.Gateway)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": result})
}

type gatewayBackup struct {
	Settings *storage.Settings `json:"settings"`
	Config   []byte            `json:"config"`
	Running  bool              `json:"running"`
}

func (s *Server) gatewayApply(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	settings := s.store.GetSettings()
	if s.platform != "linux" || settings.DeploymentRole != "gateway" {
		c.JSON(400, gin.H{"error": "只有 Linux 家庭网关可以启用"})
		return
	}
	if settings.Gateway.Enabled {
		if err := s.applyManagedConfig(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"message": "网关已经启用，策略已校验应用"})
		return
	}
	settings.Gateway = gateway.Normalize(settings.Gateway)
	settings.Gateway.Enabled = true
	candidate, err := s.buildSettings(settings, false)
	if err == nil {
		err = s.processManager.CheckConfig([]byte(candidate))
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	status, err := s.gateway.Status(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if status.Applied || status.RecoveryRequired {
		c.JSON(409, gin.H{"error": "辅助服务存在活动或未完成事务，请先恢复"})
		return
	}
	previous, err := os.ReadFile(s.resolvePath(settings.ConfigPath))
	if err != nil && !os.IsNotExist(err) {
		c.JSON(500, gin.H{"error": "读取恢复配置失败"})
		return
	}
	backup := gatewayBackup{Settings: s.store.GetSettings(), Config: previous, Running: s.processManager.IsRunning()}
	data, _ := json.Marshal(backup)
	if err = privateWrite(filepath.Join(s.store.GetDataDir(), "gateway-before.json"), data); err != nil {
		c.JSON(500, gin.H{"error": "保存网关恢复点失败"})
		return
	}
	if _, err = s.gateway.Apply(c.Request.Context(), settings.DeploymentRole, settings.Gateway); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		observed, statusErr := s.gateway.Status(ctx)
		if statusErr != nil || observed.Applied || observed.RecoveryRequired {
			recovery := s.restoreGateway(ctx, backup)
			c.JSON(500, gin.H{"error": fmt.Sprintf("辅助服务应用响应失败: %v；恢复结果: %v", err, recovery)})
			return
		}
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	err = s.processManager.ApplyConfig([]byte(candidate))
	if err == nil && !s.processManager.IsRunning() {
		err = s.processManager.Start()
	}
	if err == nil {
		data, _ = json.Marshal(settings.Gateway)
		err = privateWrite(filepath.Join(s.store.GetDataDir(), "gateway-active.json"), data)
	}
	if err == nil {
		err = s.store.UpdateSettings(settings)
	}
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		recovery := s.restoreGateway(ctx, backup)
		c.JSON(500, gin.H{"error": fmt.Sprintf("网关应用失败: %v；恢复结果: %v", err, recovery)})
		return
	}
	c.JSON(200, gin.H{"message": "网关已启用；仍需用隔离客户端验证 TCP/UDP 和 IPv4/IPv6 路由覆盖"})
}

// 失败补偿不使用可能已经取消的 HTTP context；恢复失败保留恢复点。
func (s *Server) restoreGateway(ctx context.Context, backup gatewayBackup) error {
	if s.processManager.IsRunning() {
		if err := s.processManager.Stop(); err != nil {
			return err
		}
	}
	if _, err := s.gateway.Rollback(ctx); err != nil {
		return err
	}
	var err error
	if len(backup.Config) > 0 {
		err = s.processManager.ApplyConfig(backup.Config)
	} else {
		err = os.Remove(s.resolvePath(backup.Settings.ConfigPath))
		if os.IsNotExist(err) {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	backup.Settings.Gateway.Enabled = false
	backup.Settings.AutoApply = false
	if err = s.store.UpdateSettings(backup.Settings); err != nil {
		return err
	}
	if backup.Running && len(backup.Config) > 0 {
		if err = s.processManager.Start(); err != nil {
			return err
		}
	}
	var errs []error
	for _, name := range []string{"gateway-active.json", "gateway-before.json"} {
		if e := os.Remove(filepath.Join(s.store.GetDataDir(), name)); e != nil && !os.IsNotExist(e) {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
func (s *Server) gatewayRollback(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.platform != "linux" {
		c.JSON(400, gin.H{"error": "仅 Linux 支持网关恢复"})
		return
	}
	data, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "gateway-before.json"))
	var backup gatewayBackup
	if err != nil || json.Unmarshal(data, &backup) != nil || backup.Settings == nil {
		c.JSON(400, gin.H{"error": "没有可用的网关恢复点"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = s.restoreGateway(ctx, backup); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "已恢复启用前配置和系统资源；自动应用已关闭"})
}

// 显式停止 DNS 旁路同时恢复自有系统资源；不会启动恢复点里的旧进程。
// 非预期崩溃仍保留归属记录，恢复或重新启用前由 helper 检查实际状态。
func (s *Server) stopDNSBypass(c *gin.Context) {
	data, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "gateway-before.json"))
	var backup gatewayBackup
	if err != nil || json.Unmarshal(data, &backup) != nil || backup.Settings == nil {
		c.JSON(409, gin.H{"error": "缺少 DNS 旁路恢复点，请先检查系统资源状态"})
		return
	}
	backup.Running = false
	// 普通停止保留启用后编辑的策略草案；只有显式“恢复”才回退整份设置。
	backup.Settings = s.store.GetSettings()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.restoreGateway(ctx, backup); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "DNS 旁路已停止，自有接管资源已恢复；重新启用须检查并应用。终端 DNS 和主路由静态路由仍由管理员维护。"})
}
func (s *Server) gatewayStatus(c *gin.Context) {
	settings := s.store.GetSettings()
	var helper any
	enabled := false
	warnings := []string{}
	if s.platform == "linux" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		status, err := s.gateway.Status(ctx)
		if err != nil {
			warnings = append(warnings, err.Error())
		} else {
			helper = status
			enabled = settings.Gateway.Enabled && status.Applied && !status.Drift && !status.RecoveryRequired
			for _, finding := range status.Findings {
				warnings = append(warnings, finding.Message)
			}
			if status.Drift {
				warnings = append(warnings, "系统资源与记录存在差异，网关需要恢复或人工核对")
			}
			if status.Applied && !s.processManager.IsRunning() {
				if gateway.Normalize(settings.Gateway).AccessMode == "dns" {
					warnings = append(warnings, "DNS 旁路资源仍已应用，但受管内核未运行；LAN DNS 和 FakeIP 连接不可用，请检查并恢复。真实 IP 连接仍走主路由。")
				} else {
					warnings = append(warnings, "网关系统保护已应用，但受管内核未运行；流量可能被保护规则拒绝")
				}
			}
		}
	}
	if settings.DeploymentRole == "gateway" && gateway.Normalize(settings.Gateway).AccessMode == "dns" {
		warnings = append(warnings, builder.DNSBypassWarnings(settings.Gateway))
	}
	c.JSON(200, gin.H{"data": gin.H{"platform": s.platform, "role": settings.DeploymentRole, "access_mode": gateway.Normalize(settings.Gateway).AccessMode, "enabled": enabled, "configured_enabled": settings.Gateway.Enabled, "helper": helper, "devices": devicePolicySummary(settings), "warnings": warnings}})
}

func devicePolicySummary(settings *storage.Settings) []gin.H {
	devices := []gin.H{}
	dnsBypass := settings.DeploymentRole == "gateway" && gateway.Normalize(settings.Gateway).AccessMode == "dns"
	for _, d := range storage.EffectiveDevices(settings) {
		g := storage.DeviceGroup{Policy: "split"}
		for _, v := range settings.DeviceGroups {
			if v.ID == d.GroupID {
				g = v
			}
		}
		out, dns, why := "按规则优先级匹配", "Split DNS / ProxyDNS", "普通分流（推荐）：遵循自定义规则、规则组和导入规则；国内规则可直连"
		if dnsBypass {
			dns = "直连域名真实地址 / 代理域名 IPv4 FakeIP"
			why = "普通分流：DNS 按域名策略决定路径，端口及协议规则仅作用于实际进入实例的连接"
		}
		switch g.Policy {
		case "direct":
			out = "DIRECT"
			dns = "DirectDNS，真实地址"
			why = "设备直连先于普通规则"
		case "bypass":
			out = "内核绕过 / DIRECT"
			dns = "DirectDNS，真实地址"
			why = "auto_redirect 预匹配绕过；非透明连接使用 DIRECT"
			if dnsBypass {
				out = "主路由直连 / 残留 FakeIP 使用 DIRECT"
				why = "优先返回真实 DNS 地址；旧 FakeIP 连接到达时按当前来源恢复域名并直连"
			}
		case "strict":
			out = g.Outbound
			if out == "" {
				out = "Proxy"
			}
			dns = "ProxyDNS，真实地址"
			why = "严格全代理覆盖国内直连等普通规则；代理出站不得包含直连回退"
		}
		devices = append(devices, gin.H{"id": d.ID, "name": d.Name, "addresses": d.Addresses, "enabled": d.Enabled, "policy": g.Policy, "outbound": out, "dns": dns, "explanation": why + "。这是配置解释，不能证明设备流量已经过网关。"})
	}
	return devices
}
func (s *Server) checkConfig(c *gin.Context) {
	candidate, err := s.buildConfig()
	if err == nil {
		err = s.processManager.CheckConfig([]byte(candidate))
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "候选配置校验通过，运行配置未修改"})
}
func (s *Server) configDiff(c *gin.Context) {
	candidate, err := s.buildSettings(s.store.GetSettings(), true)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	current, _ := os.ReadFile(s.resolvePath(s.store.GetSettings().ConfigPath))
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"current": redactJSON(string(current)), "candidate": redactJSON(candidate), "changed": string(current) != candidate}})
}
func redactJSON(value string) string {
	var data any
	if json.Unmarshal([]byte(value), &data) != nil {
		return ""
	}
	var walk func(any)
	walk = func(v any) {
		switch m := v.(type) {
		case map[string]any:
			for k, x := range m {
				if logger.SensitiveKey(k) {
					m[k] = "[REDACTED]"
				} else {
					walk(x)
				}
			}
		case []any:
			for _, x := range m {
				walk(x)
			}
		}
	}
	walk(data)
	b, _ := json.MarshalIndent(data, "", "  ")
	return string(b)
}

func (s *Server) restartGatewayManager(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.platform != "linux" || !s.store.GetSettings().Gateway.Enabled {
		c.JSON(400, gin.H{"error": "只有已启用的 Linux 网关支持系统服务重启"})
		return
	}
	control, ok := s.gateway.(interface {
		RestartManager(context.Context) (gateway.Status, error)
	})
	if !ok {
		c.JSON(503, gin.H{"error": "辅助服务不支持系统重启"})
		return
	}
	result, err := control.RestartManager(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": result, "message": "已提交管理服务重启，请稍后重新登录"})
}

func (s *Server) requireGatewayState(settings *storage.Settings) error {
	if settings.DeploymentRole == "gateway" {
		if s.platform != "linux" || !settings.Gateway.Enabled {
			return fmt.Errorf("先在 Linux 网关页面完成显式启用")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		status, err := s.gateway.Status(ctx)
		if err != nil {
			return fmt.Errorf("网关辅助服务状态不可用: %w", err)
		}
		plan, planErr := gateway.Preview("gateway", settings.Gateway)
		if planErr != nil {
			return planErr
		}
		if status.ConfigDigest != plan.ConfigDigest {
			return fmt.Errorf("辅助服务实际应用的网关拓扑与当前设置不一致，请恢复后重新检查")
		}
		if !status.Applied || status.Drift || status.RecoveryRequired {
			return fmt.Errorf("网关未应用、资源存在差异或需要恢复")
		}
		data, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "gateway-active.json"))
		if err != nil {
			return fmt.Errorf("缺少网关应用记录，请先检查并启用网关")
		}
		var applied gateway.Config
		if json.Unmarshal(data, &applied) != nil || !reflect.DeepEqual(gateway.Normalize(applied), gateway.Normalize(settings.Gateway)) {
			return fmt.Errorf("网关拓扑有变化，请通过网关应用流程处理")
		}
	}

	return nil
}
