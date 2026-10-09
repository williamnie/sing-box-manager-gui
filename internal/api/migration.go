package api

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/migration"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

type migrationRequest struct {
	Config      string `json:"config"`
	Hash        string `json:"hash"`
	Acknowledge bool   `json:"acknowledge_omitted"`
}

func (s *Server) migrationPreview(c *gin.Context) {
	var req migrationRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "需要配置 JSON"})
		return
	}
	report, err := migration.Preview(req.Config)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	candidate := prepareImport(s.store.Snapshot(), report.Policy)
	if _, err := s.buildData(candidate, true); err != nil {
		report.Blockers = append(report.Blockers, "配置语义校验失败: "+err.Error())
	}
	data, _ := json.Marshal(report.Policy)
	var redacted storage.ImportedPolicy
	_ = json.Unmarshal([]byte(redactJSON(string(data))), &redacted)
	report.Policy = &redacted
	c.JSON(200, gin.H{"data": report})
}
func (s *Server) migrationImport(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	var req migrationRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "需要配置 JSON"})
		return
	}
	report, err := migration.Preview(req.Config)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Hash != report.Hash || len(report.Blockers) > 0 || (len(report.Omitted) > 0 && !req.Acknowledge) {
		c.JSON(400, gin.H{"error": "请先预览相同配置、解决阻塞并确认未导入字段"})
		return
	}
	data := s.store.Snapshot()
	if data.Settings.Gateway.Enabled {
		c.JSON(409, gin.H{"error": "请先恢复活动网关，再导入配置草案"})
		return
	}
	// 备份与候选来自同一快照，避免预览校验期间两次读取拼出不同版本。
	backup, err := json.Marshal(data)
	if err != nil {
		c.JSON(500, gin.H{"error": "无法生成导入前备份"})
		return
	}
	candidate := prepareImport(data, report.Policy)
	if _, err = s.buildData(candidate, true); err != nil {
		c.JSON(400, gin.H{"error": "配置语义校验失败: " + err.Error()})
		return
	}
	if err = privateWrite(filepath.Join(s.store.GetDataDir(), "migration-before.json"), backup); err != nil {
		c.JSON(500, gin.H{"error": "无法备份导入前数据"})
		return
	}
	if err = s.store.Replace(candidate); err != nil {
		c.JSON(500, gin.H{"error": "保存配置草案失败"})
		return
	}
	c.JSON(200, gin.H{"message": "已导入配置草案，自动应用和 TUN 已关闭；运行实例未修改。请检查独立端口后校验、应用。"})
}
func (s *Server) migrationRollback(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.store.GetSettings().Gateway.Enabled {
		c.JSON(409, gin.H{"error": "请先恢复活动网关"})
		return
	}
	bytes, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "migration-before.json"))
	var data storage.AppData
	if err != nil || json.Unmarshal(bytes, &data) != nil || data.Settings == nil {
		c.JSON(400, gin.H{"error": "没有可用导入恢复点"})
		return
	}
	data.Settings.AutoApply = false
	if err = s.store.Replace(&data); err != nil {
		c.JSON(500, gin.H{"error": "恢复失败"})
		return
	}
	c.JSON(200, gin.H{"message": "导入前草案已恢复；请校验并应用以恢复运行配置"})
}

// 保留 ImportedPolicy 和恢复文件的存储格式；仅准备候选快照，不触碰运行态。
func prepareImport(data *storage.AppData, policy *storage.ImportedPolicy) *storage.AppData {
	data.Settings.ImportedPolicy = policy
	data.Settings.ProxyPlan = nil
	data.Settings.AutoApply = false
	data.Settings.Gateway.Enabled = false
	data.Settings.TunEnabled = false
	data.Settings.DeploymentRole = "desktop"
	// 原有可编辑规则和过滤器留在导入前备份，防止覆盖导入规则次序。
	data.Rules = []storage.Rule{}
	data.RuleGroups = []storage.RuleGroup{}
	data.Filters = []storage.Filter{}
	return data
}
