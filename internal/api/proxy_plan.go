package api

import (
	"encoding/json"
	"fmt"
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
	data.Settings.ProxyPlan = plan
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
	before, beforeErr := s.buildData(data, true)
	candidate, err := s.prepareProxyPlan(data, req.Plan)
	if err != nil {
		c.JSON(400, gin.H{"error": "无法整理分组：" + err.Error()})
		return
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
	c.JSON(200, gin.H{"data": gin.H{"revision": revision, "before": old, "after": next, "removed_nodes": removed, "before_error": beforeError, "auto_apply": data.Settings.AutoApply}})
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
	candidate, err := s.prepareProxyPlan(data, req.Plan)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 保存前执行可用内核校验；原始导入数据始终保留，可预览撤销方案。
	checked := false
	if _, err := s.processManager.Version(); err == nil {
		if err := s.processManager.CheckConfig([]byte(candidate)); err != nil {
			c.JSON(400, gin.H{"error": "内核校验失败，未保存：" + err.Error()})
			return
		}
		checked = true
	}
	if err := s.store.UpdateSettings(data.Settings); err != nil {
		c.JSON(500, gin.H{"error": "保存整理方案失败"})
		return
	}
	application, warning := "saved", ""
	if err := s.autoApplyConfig(); err != nil {
		application, warning = "failed", "整理方案已保存，但自动应用失败，请检查并重新应用："+err.Error()
	} else if data.Settings.AutoApply && (data.Settings.DeploymentRole != "gateway" || data.Settings.Gateway.Enabled) && s.processManager.IsRunning() {
		application = "applied"
	}
	c.JSON(200, gin.H{"data": gin.H{"application": application, "checked": checked}, "warning": warning})
}
