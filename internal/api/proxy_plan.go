package api

import (
	"encoding/json"
	"fmt"
	"github.com/xiaobei/singbox-manager/internal/migration"
	"os"
	"path/filepath"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

type proxyPlanRequest struct {
	Plan     *storage.ProxyPlan `json:"plan"`
	Revision string             `json:"revision"`
}
type proxyPlanSummary struct {
	Final  string   `json:"final"`
	Groups []string `json:"groups"`
	Nodes  []string `json:"nodes"`
}

func summarizeProxyPlan(raw string) proxyPlanSummary {
	var config struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Final string `json:"final"`
		} `json:"route"`
	}
	_ = json.Unmarshal([]byte(raw), &config)
	summary := proxyPlanSummary{Final: config.Route.Final, Groups: []string{}, Nodes: []string{}}
	for _, o := range config.Outbounds {
		tag, _ := o["tag"].(string)
		if o["type"] == "selector" || o["type"] == "urltest" {
			summary.Groups = append(summary.Groups, tag)
		} else if o["server"] != nil {
			summary.Nodes = append(summary.Nodes, tag)
		}
	}
	return summary
}
func proxyPlanRevision(data *storage.AppData, plan *storage.ProxyPlan) string {
	raw, _ := json.Marshal(struct {
		Data *storage.AppData
		Plan *storage.ProxyPlan
	}{data, plan})
	return configHash(raw)
}
func (s *Server) prepareProxyPlan(data *storage.AppData, plan *storage.ProxyPlan) (string, error) {
	if plan != nil && (plan.Primary == "" || len(plan.MergeGroups) > 2000) {
		return "", fmt.Errorf("请选择默认代理组，合并组不能超过 2000 个")
	}
	if data.Settings.ProxyPlan != nil && data.Settings.ProxyPlan.ManagedOnly && (plan == nil || !plan.ManagedOnly) {
		return "", fmt.Errorf("已统一节点来源；如需回退，请恢复迁移前配置备份")
	}
	if plan != nil && plan.ManagedOnly {
		if plan.Primary != "Proxy" {
			return "", fmt.Errorf("统一节点来源必须使用默认代理入口")
		}
		if _, err := migration.AdoptManagedNodes(data); err != nil {
			return "", err
		}
		if plan.DefaultNode != "" {
			found := false
			for _, sub := range data.Subscriptions {
				if sub.Enabled {
					for _, node := range sub.Nodes {
						if node.Tag == plan.DefaultNode {
							found = true
						}
					}
				}
			}
			for _, node := range data.ManualNodes {
				if node.Enabled && node.Node.Tag == plan.DefaultNode {
					found = true
				}
			}
			if !found {
				return "", fmt.Errorf("所选默认节点已不存在或停用，请刷新节点列表")
			}
		}
		value := *plan
		value.MergeGroups = nil
		data.Settings.ProxyPlan = &value
	} else {
		data.Settings.ProxyPlan = plan
	}
	return s.buildData(data, true)
}
func (s *Server) previewProxyPlan(c *gin.Context) {
	var req proxyPlanRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "整理方案格式无效"})
		return
	}
	data := s.store.Snapshot()
	revision := proxyPlanRevision(data, req.Plan)
	oldManualIDs := map[string]bool{}
	for _, node := range data.ManualNodes {
		oldManualIDs[node.ID] = true
	}
	before, beforeErr := s.buildData(data, true)
	candidate, err := s.prepareProxyPlan(data, req.Plan)
	if err != nil {
		c.JSON(400, gin.H{"error": "无法整理分组：" + err.Error()})
		return
	}
	if beforeErr != nil {
		if applied, err := os.ReadFile(s.resolvePath(data.Settings.ConfigPath)); err == nil {
			before = string(applied)
		}
	}
	adopted := []string{}
	for _, node := range data.ManualNodes {
		if !oldManualIDs[node.ID] {
			adopted = append(adopted, node.Node.Tag)
		}
	}
	old, next := summarizeProxyPlan(before), summarizeProxyPlan(candidate)
	removed := []string{}
	for _, tag := range old.Nodes {
		if !slices.Contains(next.Nodes, tag) {
			removed = append(removed, tag)
		}
	}
	beforeError := ""
	if beforeErr != nil {
		beforeError = beforeErr.Error()
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": gin.H{"revision": revision, "before": old, "after": next, "removed_nodes": removed, "before_error": beforeError, "auto_apply": data.Settings.AutoApply, "adopted_nodes": adopted}})
}
func (s *Server) saveProxyPlan(c *gin.Context) {
	var req proxyPlanRequest
	if c.ShouldBindJSON(&req) != nil || req.Revision == "" {
		c.JSON(400, gin.H{"error": "请先预览整理方案"})
		return
	}
	data := s.store.Snapshot()
	if req.Revision != proxyPlanRevision(data, req.Plan) {
		c.JSON(409, gin.H{"error": "节点、规则或方案已变化，请重新预览"})
		return
	}
	previous := s.store.Snapshot()
	candidate, err := s.prepareProxyPlan(data, req.Plan)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 保存前执行内核校验；统一节点来源时先写完整私有备份。
	checked := false
	if _, err := s.processManager.Version(); err == nil {
		if err := s.processManager.CheckConfig([]byte(candidate)); err != nil {
			c.JSON(400, gin.H{"error": "内核校验失败，未保存：" + err.Error()})
			return
		}
		checked = true
	}
	if req.Plan != nil && req.Plan.ManagedOnly && (previous.Settings.ProxyPlan == nil || !previous.Settings.ProxyPlan.ManagedOnly) {
		backup, err := json.Marshal(previous)
		if err != nil {
			c.JSON(500, gin.H{"error": "无法生成迁移备份"})
			return
		}
		backupPath := filepath.Join(s.store.GetDataDir(), "managed-nodes-before-"+configHash(backup)[:16]+".json")
		if err := privateWrite(backupPath, backup); err != nil {
			c.JSON(500, gin.H{"error": "无法保存迁移备份"})
			return
		}
	}
	if err := s.store.Replace(data); err != nil {
		c.JSON(500, gin.H{"error": "保存整理方案失败"})
		return
	}
	application, warning := "saved", ""
	if err := s.autoApplyConfig(); err != nil {
		if restoreErr := s.store.Replace(previous); restoreErr != nil {
			c.JSON(500, gin.H{"error": "应用失败且无法恢复草案，请检查配置：" + err.Error()})
			return
		}
		c.JSON(500, gin.H{"error": "应用失败，已恢复迁移前草案：" + err.Error()})
		return
	} else if data.Settings.AutoApply && (data.Settings.DeploymentRole != "gateway" || data.Settings.Gateway.Enabled) && s.processManager.IsRunning() {
		application = "applied"
	}
	c.JSON(200, gin.H{"data": gin.H{"application": application, "checked": checked}, "warning": warning})
}
