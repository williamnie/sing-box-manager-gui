package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

func (s *Server) logSourcePath(c *gin.Context) (string, string, bool) {
	source := c.DefaultQuery("source", "singbox")
	if source != "singbox" && source != "sbm" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日志来源只能是 singbox 或 sbm"})
		return "", "", false
	}
	return filepath.Join(s.store.GetDataDir(), "logs", source+".log"), source, true
}
func sendLogEvent(c *gin.Context, event, id string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	// 防止慢客户端永久占用请求；测试 recorder 等不支持 deadline 时忽略。
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(5 * time.Second))
	if id != "" {
		if _, err = fmt.Fprintf(c.Writer, "id: %s\n", id); err != nil {
			return false
		}
	}
	if _, err = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

func (s *Server) streamLogs(c *gin.Context) {
	path, _, ok := s.logSourcePath(c)
	if !ok {
		return
	}
	cursor := c.Query("cursor")
	// EventSource 自动重连会使用 Last-Event-ID；优先于连接创建时的 query。
	if last := c.GetHeader("Last-Event-ID"); last != "" {
		cursor = last
	}
	batch, err := logger.ReadLogBatch(path, cursor, 500)
	if err != nil {
		status := http.StatusInternalServerError
		message := "无法读取日志文件"
		if errors.Is(err, logger.ErrInvalidLogCursor) {
			status = http.StatusBadRequest
			message = err.Error()
		}
		c.JSON(status, gin.H{"error": message})
		return
	}
	if s.auth == nil || !s.auth.authenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	defer func() { _ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{}) }()
	if !sendLogEvent(c, "logs", batch.Cursor, batch) {
		return
	}
	cursor = batch.Cursor
	lastSent := time.Now()
	for {
		delay := time.Second
		if batch.More {
			delay = 50 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-c.Request.Context().Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		// 已经打开的流也必须响应登出、密码修改和会话到期。
		if !s.auth.authenticated(c) {
			sendLogEvent(c, "auth_expired", "", gin.H{"error": "登录已失效，请重新登录"})
			return
		}
		batch, err = logger.ReadLogBatch(path, cursor, 500)
		if err != nil {
			sendLogEvent(c, "stream_error", "", gin.H{"error": "日志读取中断，请重连"})
			return
		}
		if batch.Cursor != cursor || len(batch.Lines) > 0 || batch.Gap {
			if !sendLogEvent(c, "logs", batch.Cursor, batch) {
				return
			}
			cursor = batch.Cursor
			lastSent = time.Now()
		} else if time.Since(lastSent) >= 15*time.Second {
			if !sendLogEvent(c, "heartbeat", "", gin.H{"connected": true}) {
				return
			}
			lastSent = time.Now()
		}
	}
}

func (s *Server) exportLogs(c *gin.Context) {
	path, source, ok := s.logSourcePath(c)
	if !ok {
		return
	}
	limit := logger.MaxLogBatchLines
	if raw := c.Query("lines"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > logger.MaxLogBatchLines {
			c.JSON(http.StatusBadRequest, gin.H{"error": "导出行数必须在 1 到 1000 之间"})
			return
		}
		limit = value
	}
	batch, err := logger.ReadLogBatch(path, "", limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法读取日志文件"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.log"`, source, time.Now().Format("20060102-150405")))
	c.Header("X-Log-Export-Limit", strconv.Itoa(limit))
	data := strings.Join(batch.Lines, "\n")
	if len(batch.Lines) > 0 {
		data += "\n"
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(data))
}
