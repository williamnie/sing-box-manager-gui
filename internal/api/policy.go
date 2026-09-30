package api

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/gin-gonic/gin"
)

type routeSummary struct {
	Rules []map[string]any `json:"rules"`
	Final string           `json:"final"`
}

func readRouteSummary(raw []byte) (*routeSummary, error) {
	var doc struct {
		Route *routeSummary `json:"route"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Route == nil {
		return nil, fmt.Errorf("配置没有有效的 route 对象")
	}
	if doc.Route.Rules == nil {
		doc.Route.Rules = []map[string]any{}
	}
	return doc.Route, nil
}

// 草案无效或内核停止时也返回已保存的应用版本，避免空编辑列表掩盖真实规则。
func (s *Server) rulesOverview(c *gin.Context) {
	data := s.store.Snapshot()
	settings := data.Settings
	result := gin.H{
		"draft": nil, "draft_error": "", "applied": nil, "applied_error": "",
		"applied_hash": "", "changed": nil, "running": s.processManager.IsRunning(),
		"role": settings.DeploymentRole, "devices": devicePolicySummary(settings),
		"imported_rules": []map[string]any{}, "imported_rule_sets": []map[string]any{}, "imported_final": "",
	}
	if p := settings.ImportedPolicy; p != nil {
		if p.Rules != nil {
			result["imported_rules"] = p.Rules
		}
		if p.RuleSets != nil {
			result["imported_rule_sets"] = p.RuleSets
		}
		result["imported_final"] = p.Final
	}
	candidate, buildErr := s.buildData(data, true)
	if buildErr != nil {
		result["draft_error"] = buildErr.Error()
	} else {
		draft, err := readRouteSummary([]byte(candidate))
		if err != nil {
			result["draft_error"] = err.Error()
		} else {
			result["draft"] = draft
		}
	}
	current, err := os.ReadFile(s.resolvePath(settings.ConfigPath))
	if err != nil {
		if !os.IsNotExist(err) {
			result["applied_error"] = "无法读取已应用配置"
		}
	} else if applied, err := readRouteSummary(current); err != nil {
		result["applied_error"] = err.Error()
	} else {
		result["applied"] = applied
		result["applied_hash"] = configHash(current)
		if result["draft"] != nil {
			// 对完整配置作语义比较，不能因规则相同忽略 DNS、出站或接管变化。
			var oldDoc, newDoc any
			_ = json.Unmarshal(current, &oldDoc)
			_ = json.Unmarshal([]byte(candidate), &newDoc)
			result["changed"] = !reflect.DeepEqual(oldDoc, newDoc)
		}
	}
	encoded, _ := json.Marshal(result)
	// 保留扩展规则字段，但不向页面暴露其中的凭据。
	c.Data(200, "application/json; charset=utf-8", []byte(`{"data":`+redactJSON(string(encoded))+`}`))
}
