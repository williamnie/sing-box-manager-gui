package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"

	"github.com/gin-gonic/gin"
)

func configHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Server) configVersions(c *gin.Context) {
	path := s.resolvePath(s.store.GetSettings().ConfigPath)
	items := []gin.H{}
	for _, item := range []struct{ name, path string }{{"current", path}, {"previous", path + ".previous"}} {
		b, e := os.ReadFile(item.path)
		if e != nil {
			continue
		}
		st, e := os.Stat(item.path)
		if e != nil {
			continue
		}
		items = append(items, gin.H{"name": item.name, "hash": configHash(b), "updated_at": st.ModTime(), "bytes": len(b), "preview": redactJSON(string(b))})
	}
	c.JSON(200, gin.H{"data": items})
}
func (s *Server) restorePreviousConfig(c *gin.Context) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	var req struct {
		Hash string `json:"hash"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "需要预览中的版本摘要"})
		return
	}
	settings := s.store.GetSettings()
	path := s.resolvePath(settings.ConfigPath)
	previous, e := os.ReadFile(path + ".previous")
	if e != nil || configHash(previous) != req.Hash {
		c.JSON(409, gin.H{"error": "前一版本不存在或已变化，请刷新版本列表"})
		return
	}
	var old, expected map[string]any
	candidate, e := s.buildSettings(settings, false)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	if json.Unmarshal(previous, &old) != nil || json.Unmarshal([]byte(candidate), &expected) != nil {
		c.JSON(400, gin.H{"error": "配置版本格式无效"})
		return
	}
	// 版本恢复仅切换策略，不能隐式更换接管角色、监听端口或控制接口。
	for _, key := range []string{"inbounds", "experimental"} {
		if !reflect.DeepEqual(old[key], expected[key]) {
			c.JSON(409, gin.H{"error": "旧版本的监听或接管方式不同，请先恢复对应部署设置并使用部署应用流程"})
			return
		}
	}
	if settings.DeploymentRole == "gateway" {
		oldRoute, _ := old["route"].(map[string]any)
		expectedRoute, _ := expected["route"].(map[string]any)
		for _, key := range []string{"default_interface", "auto_detect_interface", "default_mark", "default_network_strategy"} {
			if !reflect.DeepEqual(oldRoute[key], expectedRoute[key]) {
				c.JSON(409, gin.H{"error": "旧版本的上游路由与当前网关不同，请使用部署恢复流程"})
				return
			}
		}
		if !reflect.DeepEqual(bypassMatches(oldRoute), bypassMatches(expectedRoute)) {
			c.JSON(409, gin.H{"error": "旧版本绕过来源与网关防火墙不同，请使用部署恢复流程"})
			return
		}
	}
	if e = s.requireGatewayState(settings); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	if e = s.processManager.CheckConfig(previous); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	original := s.store.GetSettings()
	settings.AutoApply = false
	if e = s.store.UpdateSettings(settings); e != nil {
		c.JSON(500, gin.H{"error": "无法关闭自动应用，运行版本未修改"})
		return
	}
	if e = s.processManager.ApplyConfig(previous); e != nil {
		restoreErr := s.store.UpdateSettings(original)
		c.JSON(500, gin.H{"error": fmt.Sprintf("配置恢复失败: %v；设置恢复: %v", e, restoreErr)})
		return
	}
	c.JSON(200, gin.H{"message": "前一配置版本已通过相同校验/健康事务恢复，自动应用已关闭；设置草案未覆盖"})
}

// bypass 的来源决定 helper 转发保护，恢复版本不能独立更改这些匹配。
func bypassMatches(route map[string]any) []string {
	result := []string{}
	var walk func(any)
	walk = func(v any) {
		switch m := v.(type) {
		case map[string]any:
			if m["action"] == "bypass" {
				b, _ := json.Marshal(m)
				result = append(result, string(b))
			}
			for _, x := range m {
				walk(x)
			}
		case []any:
			for _, x := range m {
				walk(x)
			}
		}
	}
	walk(route["rules"])
	sort.Strings(result)
	return result
}
