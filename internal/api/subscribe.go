package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/builder"
)

// getSubscribeInfo 获取订阅信息
func (s *Server) getSubscribeInfo(c *gin.Context) {
	settings := s.store.GetSettings()

	// 构建订阅链接
	var baseURL string
	if settings.SubscribeURL != "" {
		baseURL = settings.SubscribeURL
	} else {
		host := c.Request.Host
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if forwardedProto := c.GetHeader("X-Forwarded-Proto"); forwardedProto != "" {
			scheme = forwardedProto
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	info := gin.H{
		"enabled": settings.SubscribeEnabled,
		"token":   settings.SubscribeToken,
		"url":     settings.SubscribeURL,
		"links":   gin.H{},
	}

	if settings.SubscribeEnabled && settings.SubscribeToken != "" {
		info["links"] = gin.H{
			"singbox": fmt.Sprintf("%s/api/subscribe/singbox?token=%s", baseURL, settings.SubscribeToken),
			"clash":   fmt.Sprintf("%s/api/subscribe/clash?token=%s", baseURL, settings.SubscribeToken),
		}
	}

	c.JSON(http.StatusOK, info)
}

// generateSubscribeToken 生成订阅令牌
func (s *Server) generateSubscribeToken(c *gin.Context) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
		return
	}
	token := hex.EncodeToString(bytes)

	// 保存到设置
	settings := s.store.GetSettings()
	settings.SubscribeToken = token
	if err := s.store.UpdateSettings(settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存令牌失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}

// getSingboxSubscribe 获取 sing-box 格式订阅
func (s *Server) getSingboxSubscribe(c *gin.Context) {
	settings := s.store.GetSettings()

	// 验证令牌
	token := c.Query("token")
	if !settings.SubscribeEnabled || token == "" || token != settings.SubscribeToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的订阅令牌"})
		return
	}

	// 获取所有节点
	nodes := s.store.GetAllNodes()
	if len(nodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用节点"})
		return
	}

	// 使用移动端模式生成配置
	configBuilder := builder.NewConfigBuilder(
		settings,
		nodes,
		s.store.GetFilters(),
		s.store.GetRules(),
		s.store.GetRuleGroups(),
	)

	opts := builder.BuildOptions{MobileMode: true}
	configJSON, err := configBuilder.BuildJSONWithOptions(opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("生成配置失败: %v", err)})
		return
	}

	// 添加订阅信息头
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=singbox.json")
	c.Header("Profile-Update-Interval", "24")
	c.String(http.StatusOK, configJSON)
}

// getClashSubscribe 获取 Clash 格式订阅
func (s *Server) getClashSubscribe(c *gin.Context) {
	settings := s.store.GetSettings()

	// 验证令牌
	token := c.Query("token")
	if !settings.SubscribeEnabled || token == "" || token != settings.SubscribeToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的订阅令牌"})
		return
	}

	// 获取所有节点
	nodes := s.store.GetAllNodes()
	if len(nodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用节点"})
		return
	}

	// 使用 Clash 转换器
	converter := builder.NewClashConverter(
		settings,
		nodes,
		s.store.GetFilters(),
		s.store.GetRules(),
		s.store.GetRuleGroups(),
	)

	configYAML, err := converter.ConvertYAML()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("生成配置失败: %v", err)})
		return
	}

	// 添加订阅信息头
	c.Header("Content-Type", "text/yaml; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=clash.yaml")
	c.Header("Profile-Update-Interval", "24")
	c.String(http.StatusOK, configYAML)
}

// exportSubscribe 导出订阅配置文件
func (s *Server) exportSubscribe(c *gin.Context) {
	var req struct {
		Format string `json:"format"` // singbox 或 clash
		Path   string `json:"path"`   // 导出路径（可选）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}

	settings := s.store.GetSettings()
	nodes := s.store.GetAllNodes()
	if len(nodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用节点"})
		return
	}

	var content string
	var filename string
	var err error

	switch req.Format {
	case "singbox":
		configBuilder := builder.NewConfigBuilder(
			settings, nodes,
			s.store.GetFilters(),
			s.store.GetRules(),
			s.store.GetRuleGroups(),
		)
		opts := builder.BuildOptions{MobileMode: true}
		content, err = configBuilder.BuildJSONWithOptions(opts)
		filename = "singbox.json"
	case "clash":
		converter := builder.NewClashConverter(
			settings, nodes,
			s.store.GetFilters(),
			s.store.GetRules(),
			s.store.GetRuleGroups(),
		)
		content, err = converter.ConvertYAML()
		filename = "clash.yaml"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的格式"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("生成配置失败: %v", err)})
		return
	}

	// 如果指定了路径，保存到文件
	if req.Path != "" {
		exportPath := filepath.Join(req.Path, filename)
		if err := os.WriteFile(exportPath, []byte(content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存文件失败: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "导出成功", "path": exportPath})
		return
	}

	// 否则直接返回内容
	c.JSON(http.StatusOK, gin.H{"content": content, "filename": filename})
}
