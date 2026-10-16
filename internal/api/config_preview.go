package api

import (
	"fmt"
	"net/http"
	"os"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const configPreviewMaxBytes = 16 * 1024
const configPreviewMaxLines = 200

type configPreviewInfo struct {
	Bytes        int    `json:"bytes"`
	Hash         string `json:"hash"`
	PreviewBytes int    `json:"preview_bytes"`
	Truncated    bool   `json:"truncated"`
}

// 只截取已脱敏的展示文本；摘要、比较、校验和应用始终使用完整原文。
// 同时限制字节与行数，避免单条超长规则或大量短行撑大布局。
func configPreview(redacted string, raw []byte) (string, configPreviewInfo) {
	end := min(len(redacted), configPreviewMaxBytes)
	for end < len(redacted) && end > 0 && !utf8.RuneStart(redacted[end]) {
		end--
	}
	lines := 1
	for i := 0; i < end; i++ {
		if redacted[i] == '\n' {
			if lines == configPreviewMaxLines {
				end = i
				break
			}
			lines++
		}
	}
	return redacted[:end], configPreviewInfo{Bytes: len(raw), Hash: configHash(raw), PreviewBytes: end, Truncated: end < len(redacted)}
}

// 浏览器直接保存附件，不把完整配置再次解码或挂载到页面 DOM。
func (s *Server) downloadConfig(c *gin.Context) {
	source := c.Query("source")
	var raw []byte
	var err error
	switch source {
	case "candidate":
		var candidate string
		candidate, err = s.buildSettings(s.store.GetSettings(), true)
		raw = []byte(candidate)
	case "current", "previous":
		path := s.resolvePath(s.store.GetSettings().ConfigPath)
		if source == "previous" {
			path += ".previous"
		}
		raw, err = os.ReadFile(path)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置来源无效"})
		return
	}
	c.Header("Cache-Control", "no-store")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取或构建配置，请刷新预览"})
		return
	}
	if c.Query("hash") != configHash(raw) {
		c.JSON(http.StatusConflict, gin.H{"error": "配置已变化或缺少摘要，请刷新预览后重新下载"})
		return
	}
	redacted := redactJSON(string(raw))
	if redacted == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置不是有效 JSON，无法安全脱敏下载"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="sing-box-%s-redacted.json"`, source))
	c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(redacted))
}
