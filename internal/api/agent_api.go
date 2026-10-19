package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/gateway"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

const agentAPIVersion = "v1"

// 自动回滚的真实保证范围，capabilities、界面与 Skill 使用同一说明。
const confirmGuarantee = "确认窗口由管理器进程计时：管理器运行期间到期未确认会自动回滚；管理器重启时在开放 API 与订阅调度前优先恢复所有未确认变更。管理器崩溃或停机期间没有独立 watchdog，不保证准时回滚；主机重启后的恢复只还原数据与停止态的配置文件，不会自动启动内核。"

func (s *Server) setupAgentRoutes() {
	// Agent 电脑上的一键接入：脚本与客户端都是公开内容，不要求登录，仍受来源网段限制。
	s.router.GET("/agent/setup.sh", s.agentSetupScript)
	s.router.GET("/agent/download/:name", s.agentDownload)
	v1 := s.router.Group("/api/agent/v1", s.requireAgentToken)
	read, runtime, write, deploy := requireScope(scopeRead), requireScope(scopeRuntime), requireScope(scopeWrite), requireScope(scopeDeploy)
	v1.GET("/capabilities", s.agentCapabilities)
	v1.GET("/status", read, s.agentStatus)
	v1.GET("/config/diff", read, s.agentConfigDiff)
	v1.GET("/config/versions", read, redacted(s.configVersions))
	v1.GET("/rules/overview", read, redacted(s.rulesOverview))
	v1.GET("/resources/:kind", read, s.agentListResource)
	v1.GET("/nodes", read, s.agentNodes)
	v1.GET("/explain/domain", read, s.agentExplainDomain)
	v1.GET("/explain/client", read, s.agentExplainClient)
	v1.GET("/logs", read, s.agentLogs)
	v1.GET("/dns/queries", read, redacted(s.getDNSQueries))
	v1.GET("/gateway/status", read, redacted(s.gatewayStatus))
	v1.GET("/gateway/clients", read, redacted(s.gatewayClients))
	v1.GET("/gateway/plan", read, s.agentGatewayPlan)
	v1.GET("/kernel", read, redacted(s.getKernelInfo))
	v1.GET("/runtime/proxies", read, redacted(s.runtimeProxies))
	v1.GET("/runtime/connections", read, redacted(s.runtimeConnections))
	v1.POST("/runtime/select", runtime, s.agentGate, redacted(s.runtimeSelect))
	v1.POST("/runtime/delay", runtime, redacted(s.runtimeDelay))
	v1.GET("/changesets", read, s.agentChangesets)
	v1.GET("/changesets/:id", read, s.agentChangeset)
	v1.POST("/changesets", write, s.agentCreateChangeset)
	v1.POST("/changesets/:id/apply", deploy, s.agentLocked, s.agentApplyChangeset)
	v1.POST("/changesets/:id/confirm", deploy, s.agentLocked, s.agentConfirmChangeset)
	v1.POST("/changesets/:id/rollback", deploy, s.agentLocked, s.agentRollbackChangeset)
	v1.GET("/audit", read, s.getAudit)
}

// setupAgentAdminRoutes 挂在管理员会话路由组下；令牌不能调用。
func (s *Server) setupAgentAdminRoutes(api *gin.RouterGroup) {
	api.GET("/agent/connection", s.agentConnection)
	api.GET("/agent/tokens", s.listAgentTokens)
	api.POST("/agent/tokens", s.createAgentToken)
	api.DELETE("/agent/tokens/:id", s.revokeAgentToken)
	api.GET("/agent/state", s.agentAdminState)
	api.GET("/agent/changesets", s.agentChangesets)
	api.GET("/agent/changesets/:id", s.agentChangeset)
	api.POST("/agent/changesets/:id/confirm", s.agentConfirmChangeset)
	api.POST("/agent/changesets/:id/rollback", s.agentRollbackChangeset)
	api.POST("/agent/changesets/:id/resolve", s.agentResolveChangeset)
	api.POST("/agent/changesets/:id/revert", s.agentAdminRevert)
	api.POST("/agent/changesets/:id/apply", s.agentApplyChangeset)
	api.GET("/agent/audit", s.getAudit)
}

// agentLocked 让 Agent 的应用/确认/回滚与管理界面写入共用 writeMu。
func (s *Server) agentLocked(c *gin.Context) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	c.Next()
}

func (s *Server) agentGate(c *gin.Context) {
	if s.agentGateResponse(c) {
		return
	}
	c.Next()
}

type capturedWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func (w *capturedWriter) Write(b []byte) (int, error)       { return w.body.Write(b) }
func (w *capturedWriter) WriteString(s string) (int, error) { return w.body.WriteString(s) }
func (w *capturedWriter) WriteHeader(code int)              { w.status = code }
func (w *capturedWriter) WriteHeaderNow()                   {}

// redacted 复用界面处理函数，并在返回 Agent 前对 JSON 统一脱敏。
func redacted(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		original := c.Writer
		w := &capturedWriter{ResponseWriter: original, status: 200}
		c.Writer = w
		handler(c)
		c.Writer = original
		var payload any
		if json.Unmarshal(w.body.Bytes(), &payload) != nil {
			c.AbortWithStatusJSON(w.status, gin.H{"error": logger.Redact(w.body.String())})
			return
		}
		c.JSON(w.status, redactAny(payload))
	}
}

func writeAgentError(c *gin.Context, err error) {
	var ae *agentError
	if errors.As(err, &ae) {
		body := gin.H{"error": logger.Redact(ae.Msg), "code": ae.Code}
		for k, v := range ae.Extra {
			body[k] = redactedJSON(v)
		}
		c.JSON(ae.Status, body)
		return
	}
	c.JSON(500, gin.H{"error": logger.Redact(err.Error()), "code": "internal"})
}

func (s *Server) agentCapabilities(c *gin.Context) {
	token := c.MustGet("agent_token").(agentToken)
	c.JSON(200, gin.H{"data": gin.H{
		"api_version": agentAPIVersion, "sbm_version": s.version, "platform": s.platform,
		"token": gin.H{"id": token.ID, "name": token.Name, "scopes": token.Scopes, "expires_at": token.ExpiresAt},
		"scopes": gin.H{
			scopeRead:    "读取状态、配置差异、资源、节点、日志、DNS 记录、解释、变更集与审计（输出脱敏）",
			scopeRuntime: "切换选择组节点、测延迟；不改持久配置，确认窗口内切换节点会被拒绝",
			scopeWrite:   "创建并校验变更集（不修改运行配置）",
			scopeDeploy:  "应用、确认、回滚本令牌创建的变更集",
		},
		"resources": agentResources,
		"changeset": gin.H{
			"flow":            []string{"POST /changesets（预览+校验，返回 candidate_config 摘要）", "POST /changesets/{id}/apply（进入确认窗口）", "验证流量后 POST /changesets/{id}/confirm，或 /rollback"},
			"confirm_seconds": gin.H{"default": defaultConfirmSeconds, "min": minConfirmSeconds, "max": maxConfirmSeconds},
			"guarantee":       confirmGuarantee,
			"pending_policy":  "存在未应用的已保存改动时默认拒绝应用；需要一并应用时以 include_pending=<差异摘要> 重新创建",
			"idempotency":     "创建与应用支持 Idempotency-Key 请求头（绑定令牌与内容摘要；同 key 不同内容返回 422；重试返回已记录结果，不重复执行）",
			"concurrency":     "变更集绑定创建时的 base_rev；配置在此后被修改则应用返回 409 base_changed",
			"ttl_seconds":     int(changesetTTL.Seconds()),
		},
		"evidence_levels": gin.H{
			"saved":   "已保存到管理器数据（data.json）",
			"applied": "已写入运行配置 generated/config.json；内核运行时随应用重启生效，内核停止时不会被启动",
			"runtime": "内核实时连接/选择组/DNS 查询记录，是真实流量证据",
		},
		"not_supported": []string{"网关启用/恢复与拓扑变更（含影响 bypass 网段的设备变更）", "内核下载与替换", "root 策略修改", "端口、路径、监听、自动应用等系统设置", "令牌管理与改密", "导入迁移与代理整理方案"},
	}})
}

func (s *Server) agentStatus(c *gin.Context) {
	data := s.store.Snapshot()
	settings := data.Settings
	drift, driftErr := s.pendingDrift(data)
	applied, exists, _ := s.readAppliedConfig()
	version, versionErr := s.processManager.Version()
	running := s.processManager.IsRunning()
	pending := gin.H{"exists": drift != nil}
	if drift != nil {
		pending["digest"], pending["changes"] = drift.Digest, drift.Changes
	}
	if driftErr != nil {
		pending["error"] = driftErr.Error()
	}
	var active any
	if cs := s.agentBlocking(); cs != nil {
		active = changesetView(cs)
	}
	var next *time.Time
	if s.scheduler != nil {
		next = s.scheduler.GetNextUpdateTime()
	}
	c.JSON(200, gin.H{"data": gin.H{
		"sbm_version": s.version, "platform": s.platform, "role": settings.DeploymentRole,
		"gateway": gin.H{"access_mode": gateway.Normalize(settings.Gateway).AccessMode, "configured_enabled": settings.Gateway.Enabled},
		"core":    gin.H{"running": running, "pid": s.processManager.GetPID(), "kernel_installed": versionErr == nil, "version": version},
		"config": gin.H{
			"saved_rev": storeDigest(data), "applied_exists": exists, "applied_digest": configDigest(applied),
			"pending": pending,
			"note":    "saved=已保存；applied=运行配置文件；内核运行时 applied 即生效配置，内核停止时不产生流量",
		},
		"auto_apply":            settings.AutoApply,
		"subscription_interval": settings.SubscriptionInterval,
		"next_subscription_run": next,
		"active_operation":      active,
		"journal_error":         journalError(s),
	}})
}

func journalError(s *Server) string {
	if s.agent != nil && s.agent.err != nil {
		return s.agent.err.Error()
	}
	return ""
}

func (s *Server) agentConfigDiff(c *gin.Context) {
	data := s.store.Snapshot()
	drift, err := s.pendingDrift(data)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error(), "code": "pending_unknown"})
		return
	}
	if drift == nil {
		c.JSON(200, gin.H{"data": gin.H{"pending": false, "changes": []string{}}})
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"pending": true, "digest": drift.Digest, "applied_exists": drift.AppliedExists, "changes": drift.Changes,
		"hint": "这些是已保存但未应用的改动（可能来自管理界面）。Agent 变更默认不夹带；确认要一并应用时以 include_pending=digest 创建变更集"}})
}

func (s *Server) agentListResource(c *gin.Context) {
	data := s.store.Snapshot()
	settings := data.Settings
	var value any
	switch c.Param("kind") {
	case "rule":
		value = data.Rules
	case "rule_group":
		value = data.RuleGroups
	case "filter":
		value = data.Filters
	case "subscription":
		subs := []any{}
		for _, sub := range data.Subscriptions {
			subs = append(subs, gin.H{"id": sub.ID, "name": sub.Name, "url": sub.URL, "enabled": sub.Enabled, "node_count": len(sub.Nodes), "updated_at": sub.UpdatedAt, "expire_at": sub.ExpireAt, "traffic": sub.Traffic})
		}
		value = subs
	case "manual_node":
		value = data.ManualNodes
	case "host":
		value = settings.Hosts
	case "split_dns":
		value = settings.SplitDNS
	case "device_group":
		value = settings.DeviceGroups
	case "device":
		value = settings.Devices
	case "dns":
		value = gin.H{"proxy_dns": settings.ProxyDNS, "direct_dns": settings.DirectDNS, "final_outbound": settings.FinalOutbound, "log_level": settings.LogLevel}
	case "blocklist":
		value = s.domainBlocklistData()
	case "imported_rule":
		policy := settings.ImportedPolicy
		result := gin.H{"revision": importedPolicyRevision(policy), "rules": []map[string]any{}, "rule_sets": []map[string]any{}, "final": ""}
		if policy != nil {
			result["rules"], result["rule_sets"], result["final"] = policy.Rules, policy.RuleSets, policy.Final
		}
		value = result
	default:
		c.JSON(404, gin.H{"error": "未知资源", "code": "not_found"})
		return
	}
	c.JSON(200, gin.H{"data": redactedJSON(value), "evidence": "saved"})
}

func (s *Server) agentNodes(c *gin.Context) {
	data := s.store.Snapshot()
	nodes := []gin.H{}
	for _, sub := range data.Subscriptions {
		for _, n := range sub.Nodes {
			nodes = append(nodes, gin.H{"tag": n.Tag, "type": n.Type, "server": n.Server, "server_port": n.ServerPort, "country": n.Country, "source": "subscription:" + sub.ID, "enabled": sub.Enabled})
		}
	}
	for _, m := range data.ManualNodes {
		nodes = append(nodes, gin.H{"tag": m.Node.Tag, "type": m.Node.Type, "server": m.Node.Server, "server_port": m.Node.ServerPort, "country": m.Node.Country, "source": "manual:" + m.ID, "enabled": m.Enabled})
	}
	if country := strings.ToUpper(c.Query("country")); country != "" {
		filtered := []gin.H{}
		for _, n := range nodes {
			if n["country"] == country {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}
	c.JSON(200, gin.H{"data": nodes, "evidence": "saved"})
}

func (s *Server) agentLogs(c *gin.Context) {
	lines, _ := strconv.Atoi(c.DefaultQuery("lines", "200"))
	if lines < 1 || lines > 2000 {
		lines = 200
	}
	var (
		out []string
		err error
	)
	source := c.DefaultQuery("source", "singbox")
	switch source {
	case "singbox":
		out, err = logger.ReadSingboxLogs(lines)
	case "app":
		out, err = logger.ReadAppLogs(lines)
	default:
		c.JSON(400, gin.H{"error": "source 只能是 singbox 或 app", "code": "bad_request"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "读取日志失败", "code": "internal"})
		return
	}
	grep := strings.ToLower(c.Query("grep"))
	result := []string{}
	for _, line := range out {
		line = logger.Redact(line)
		if grep == "" || strings.Contains(strings.ToLower(line), grep) {
			result = append(result, line)
		}
	}
	c.JSON(200, gin.H{"data": result, "source": source, "evidence": "日志只包含最近 " + strconv.Itoa(lines) + " 行内核/管理器输出，已脱敏；不等同于完整流量记录"})
}

func (s *Server) agentGatewayPlan(c *gin.Context) {
	settings := s.store.GetSettings()
	plan, err := gateway.Preview(settings.DeploymentRole, settings.Gateway)
	result := gin.H{"plan": redactedJSON(plan), "role": settings.DeploymentRole, "note": "只读预览：Agent 不能启用、恢复或修改网关拓扑"}
	if err != nil {
		result["error"] = err.Error()
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Server) agentChangesets(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	c.JSON(200, gin.H{"data": s.listChangesets(limit)})
}

func (s *Server) agentChangeset(c *gin.Context) {
	view, ok := s.getChangesetView(c.Param("id"))
	if !ok {
		c.JSON(404, gin.H{"error": "变更集不存在", "code": "not_found"})
		return
	}
	c.JSON(200, gin.H{"data": view})
}

func (s *Server) agentCreateChangeset(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(413, gin.H{"error": "请求过大", "code": "bad_request"})
		return
	}
	actor, tokenID := agentActor(c)
	cs, replay, err := s.createChangeset(actor, tokenID, c.GetHeader("Idempotency-Key"), body)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	if !replay {
		s.audit(actor, "changeset.create", cs.ID, cs.State, strings.Join(cs.Changes, "; "))
	}
	c.JSON(200, gin.H{"data": changesetView(cs), "idempotent_replay": replay})
}

func (s *Server) agentApplyChangeset(c *gin.Context) {
	var req struct {
		ConfirmSeconds int `json:"confirm_seconds"`
	}
	if err := strictDecode(readBody(c), &req); err != nil {
		writeAgentError(c, agentFail(400, "bad_request", "%s", err.Error()))
		return
	}
	actor, tokenID := agentActor(c)
	cs, replay, err := s.applyChangeset(c.Param("id"), actor, tokenID, c.GetHeader("Idempotency-Key"), req.ConfirmSeconds)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	status := http.StatusOK
	if cs.State == csFailed || cs.State == csRecovery {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"data": changesetView(cs), "idempotent_replay": replay, "guarantee": confirmGuarantee})
}

func readBody(c *gin.Context) []byte {
	body, _ := io.ReadAll(c.Request.Body)
	return body
}

func (s *Server) agentConfirmChangeset(c *gin.Context) {
	var req struct {
		CandidateConfig string `json:"candidate_config"`
	}
	if err := strictDecode(readBody(c), &req); err != nil {
		writeAgentError(c, agentFail(400, "bad_request", "%s", err.Error()))
		return
	}
	actor, tokenID := agentActor(c)
	cs, replay, err := s.confirmChangeset(c.Param("id"), actor, tokenID, req.CandidateConfig)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": changesetView(cs), "idempotent_replay": replay})
}

func (s *Server) agentRollbackChangeset(c *gin.Context) {
	actor, tokenID := agentActor(c)
	s.operationMu.Lock()
	cs, err := s.rollbackLocked(c.Param("id"), actor, tokenID, "手动回滚")
	s.operationMu.Unlock()
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": changesetView(cs)})
}

func (s *Server) agentResolveChangeset(c *gin.Context) {
	var req struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal(readBody(c), &req)
	if strings.TrimSpace(req.Note) == "" {
		c.JSON(400, gin.H{"error": "请填写人工核对说明", "code": "bad_request"})
		return
	}
	cs, err := s.resolveChangeset(c.Param("id"), req.Note)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": changesetView(cs)})
}

// agentAdminRevert 由管理员在界面上发起撤销：创建并校验一个新的撤销变更集，仍需应用并确认。
func (s *Server) agentAdminRevert(c *gin.Context) {
	body, _ := json.Marshal(gin.H{"title": "撤销变更 " + c.Param("id"), "ops": []gin.H{{"action": "apply", "resource": "revert", "id": c.Param("id")}}})
	cs, _, err := s.createChangeset("admin", "", "", body)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	s.audit("admin", "changeset.create", cs.ID, cs.State, strings.Join(cs.Changes, "; "))
	c.JSON(200, gin.H{"data": changesetView(cs)})
}

func (s *Server) agentAdminState(c *gin.Context) {
	var active any
	var remainingMS int64
	if cs := s.agentBlocking(); cs != nil {
		active = changesetView(cs)
		if cs.Deadline != nil {
			remainingMS = max(int64(0), time.Until(*cs.Deadline).Milliseconds())
		}
	}
	c.JSON(200, gin.H{"data": gin.H{"active": active, "remaining_ms": remainingMS, "journal_error": journalError(s), "guarantee": confirmGuarantee}})
}
