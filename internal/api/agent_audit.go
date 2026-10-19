package api

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

const maxAuditBytes = 4 << 20

type auditEntry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Target string    `json:"target,omitempty"`
	Result string    `json:"result"`
	Detail string    `json:"detail,omitempty"`
}

type auditLog struct {
	mu   sync.Mutex
	path string
}

// audit 写失败不阻断已完成的操作，只记入程序日志；明细统一脱敏。
func (s *Server) audit(actor, action, target, result, detail string) {
	if s.auditLog == nil {
		return
	}
	entry := auditEntry{At: time.Now().UTC(), Actor: actor, Action: action, Target: logger.Redact(target), Result: result, Detail: logger.Redact(detail)}
	if len(entry.Detail) > 2000 {
		entry.Detail = entry.Detail[:2000] + "…"
	}
	line, _ := json.Marshal(entry)
	a := s.auditLog
	a.mu.Lock()
	defer a.mu.Unlock()
	if info, err := os.Stat(a.path); err == nil && info.Size() > maxAuditBytes {
		_ = os.Rename(a.path, a.path+".1")
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		logger.Printf("写入审计日志失败")
		return
	}
	defer f.Close()
	if _, err = f.Write(append(line, '\n')); err == nil {
		err = f.Sync()
	}
	if err != nil {
		logger.Printf("写入审计日志失败")
	}
}

func (a *auditLog) read(limit int) []auditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	entries := []auditEntry{}
	for _, path := range []string{a.path + ".1", a.path} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), 64<<10)
		for scanner.Scan() {
			var e auditEntry
			if json.Unmarshal(scanner.Bytes(), &e) == nil {
				entries = append(entries, e)
			}
		}
		f.Close()
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	// 最新的在前。
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

func newAuditLog(dir string) *auditLog {
	return &auditLog{path: filepath.Join(dir, "audit.jsonl")}
}

func (s *Server) getAudit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit < 1 || limit > 2000 {
		limit = 200
	}
	c.JSON(200, gin.H{"data": s.auditLog.read(limit)})
}
