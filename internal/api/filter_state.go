package api

import (
	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 过滤器开关必须与有效草案一致；失败时保留旧状态，避免界面显示关闭而内核仍运行旧组。
func (s *Server) saveFilterChange(c *gin.Context, before, candidate *storage.AppData, filter *storage.Filter) {
	raw, err := s.buildData(candidate, true)
	if err != nil {
		c.JSON(400, gin.H{"error": "过滤器未更改：" + err.Error() + "。请先调整引用该组的规则或默认出口。"})
		return
	}
	if _, err := s.processManager.Version(); err == nil {
		if err := s.processManager.CheckConfig([]byte(raw)); err != nil {
			c.JSON(400, gin.H{"error": "过滤器未更改，内核校验失败：" + err.Error()})
			return
		}
	}
	if err := s.store.Replace(candidate); err != nil {
		c.JSON(500, gin.H{"error": "保存过滤器失败"})
		return
	}
	if err := s.autoApplyConfig(); err != nil {
		if restoreErr := s.store.Replace(before); restoreErr != nil {
			c.JSON(500, gin.H{"error": "应用失败且无法恢复草案，请检查配置：" + err.Error()})
			return
		}
		c.JSON(500, gin.H{"error": "应用失败，过滤器已恢复原状态：" + err.Error()})
		return
	}
	application := "saved"
	if candidate.Settings.AutoApply && (candidate.Settings.DeploymentRole != "gateway" || candidate.Settings.Gateway.Enabled) && s.processManager.IsRunning() {
		application = "applied"
	}
	c.JSON(200, gin.H{"data": filter, "application": application, "message": "过滤器已保存"})
}
