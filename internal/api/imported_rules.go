package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 只允许编辑表单支持的字段；未显示的扩展字段留在原对象中。
var importedRuleFields = map[string]string{
	"domain": "text", "domain_suffix": "text", "domain_keyword": "text", "domain_regex": "text",
	"ip_cidr": "text", "source_ip_cidr": "text", "rule_set": "text", "network": "text", "protocol": "text", "process_name": "text",
	"port": "ports", "source_port": "ports", "port_range": "text", "source_port_range": "text",
	"invert": "bool", "ip_is_private": "bool", "source_ip_is_private": "bool",
	"action": "single", "outbound": "single",
}

func importedPolicyRevision(p *storage.ImportedPolicy) string {
	raw, _ := json.Marshal(p)
	return configHash(raw)
}

func validateImportedField(key string, value any) error {
	typ, ok := importedRuleFields[key]
	if !ok {
		return fmt.Errorf("不支持编辑字段 %s，其他条件会保留原值", key)
	}
	if value == nil {
		return nil
	}
	if typ == "bool" {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s 必须是开关值", key)
		}
		return nil
	}
	if typ == "single" {
		v, ok := value.(string)
		if !ok || strings.TrimSpace(v) == "" || len(v) > 1024 {
			return fmt.Errorf("%s 必须是非空文本", key)
		}
		if key == "action" && v != "route" && v != "reject" {
			return fmt.Errorf("只支持切换为指定出站或拒绝动作")
		}
		return nil
	}
	items, ok := value.([]any)
	if !ok || len(items) > 5000 {
		return fmt.Errorf("%s 必须是最多 5000 项的列表", key)
	}
	for _, item := range items {
		if typ == "ports" {
			port, ok := item.(float64)
			if !ok || port < 1 || port > 65535 || port != float64(int(port)) {
				return fmt.Errorf("端口必须是 1–65535 的整数")
			}
			continue
		}
		v, ok := item.(string)
		if !ok || strings.TrimSpace(v) == "" || len(v) > 2048 {
			return fmt.Errorf("%s 列表包含空值或无效文本", key)
		}
		switch key {
		case "domain", "domain_suffix":
			if err := storage.ValidateRule(storage.Rule{RuleType: key, Values: []string{v}, Outbound: "REJECT"}, false); err != nil {
				return err
			}
		case "ip_cidr", "source_ip_cidr":
			if _, err := storage.SourcePrefix(v); err != nil {
				return fmt.Errorf("IP 或 CIDR 无效：%s", v)
			}
		case "port_range", "source_port_range":
			if err := storage.ValidateRule(storage.Rule{RuleType: "port_range", Values: []string{v}, Outbound: "REJECT"}, false); err != nil {
				return err
			}
		case "domain_regex":
			if _, err := regexp.Compile(v); err != nil {
				return fmt.Errorf("域名表达式无效：%s", v)
			}
		case "network":
			if v != "tcp" && v != "udp" {
				return fmt.Errorf("传输类型只能是 tcp 或 udp")
			}
		}
	}
	return nil
}

func hasImportedMatch(rule map[string]any) bool {
	for key, value := range rule {
		if key == "rules" {
			if values, ok := value.([]any); ok && len(values) > 0 {
				return true
			}
		}
		if key == "action" || key == "outbound" || key == "invert" {
			continue
		}
		if typ := importedRuleFields[key]; typ != "" {
			switch v := value.(type) {
			case []any:
				if len(v) > 0 {
					return true
				}
			case string:
				if v != "" {
					return true
				}
			case float64:
				if v > 0 {
					return true
				}
			case bool:
				if v {
					return true
				}
			}
		}
	}
	return false
}

func (s *Server) updateImportedRule(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		c.JSON(400, gin.H{"error": "规则位置无效"})
		return
	}
	var req struct {
		Revision string         `json:"revision"`
		Updates  map[string]any `json:"updates"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Updates) == 0 {
		c.JSON(400, gin.H{"error": "请提供需要修改的规则字段"})
		return
	}
	// 写请求由 writeMu 串行化；版本覆盖整个导入策略，防止重新导入或排序后编辑错行。
	data := s.store.Snapshot()
	policy := data.Settings.ImportedPolicy
	if policy == nil || index >= len(policy.Rules) {
		c.JSON(404, gin.H{"error": "导入规则已不存在，请刷新列表"})
		return
	}
	if req.Revision != importedPolicyRevision(policy) {
		c.JSON(409, gin.H{"error": "导入策略已在其他页面更新，请关闭弹窗并刷新列表后再编辑；当前输入已保留"})
		return
	}
	current := policy.Rules[index]
	rule := make(map[string]any, len(current))
	for key, value := range current {
		rule[key] = value
	}
	for key, value := range req.Updates {
		if err := validateImportedField(key, value); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if value == nil {
			delete(rule, key)
		} else {
			rule[key] = value
		}
	}
	if hasImportedMatch(current) && !hasImportedMatch(rule) {
		c.JSON(400, gin.H{"error": "请至少保留一个匹配条件，不能将这条规则清空为全部流量"})
		return
	}
	if !reflect.DeepEqual(rule["action"], current["action"]) {
		if old, _ := current["action"].(string); old != "" && old != "route" && old != "reject" {
			c.JSON(400, gin.H{"error": "此规则使用高级动作，只能修改匹配条件，不能直接切换处理方式"})
			return
		}
		for _, key := range []string{"override_address", "override_port", "network_strategy", "fallback_delay", "udp_disable_domain_unmapping", "udp_connect", "udp_timeout", "tls_fragment", "tls_fragment_fallback_delay", "tls_record_fragment"} {
			if current[key] != nil {
				c.JSON(400, gin.H{"error": "此规则带有连接处理参数，请保留当前动作，仅编辑匹配条件"})
				return
			}
		}
		if current["action"] == "reject" {
			delete(rule, "method")
			delete(rule, "no_drop")
		}
	}
	if rule["action"] == "reject" {
		if rule["outbound"] != nil {
			c.JSON(400, gin.H{"error": "拒绝动作不应指定出站"})
			return
		}
	} else if rule["action"] == nil || rule["action"] == "route" {
		if out, _ := rule["outbound"].(string); out == "" {
			c.JSON(400, gin.H{"error": "请选择目标出站"})
			return
		}
	}
	if reflect.DeepEqual(current, rule) {
		c.JSON(200, gin.H{"message": "规则内容未变化", "application": "unchanged"})
		return
	}
	policy.Rules[index] = rule
	candidate, err := s.buildData(data, true)
	if err != nil {
		c.JSON(400, gin.H{"error": "规则无法生成有效配置：" + err.Error()})
		return
	}
	// 验证本次修改的引用，允许指向管理器或导入策略中的规则集。
	if refs, changed := req.Updates["rule_set"]; changed && refs != nil {
		var config struct {
			Route struct {
				RuleSets []struct {
					Tag string `json:"tag"`
				} `json:"rule_set"`
			} `json:"route"`
		}
		_ = json.Unmarshal([]byte(candidate), &config)
		tags := map[string]bool{}
		for _, set := range config.Route.RuleSets {
			tags[set.Tag] = true
		}
		for _, ref := range refs.([]any) {
			if !tags[ref.(string)] {
				c.JSON(400, gin.H{"error": "规则集不存在：" + ref.(string)})
				return
			}
		}
	}
	checked := false
	if _, err := s.processManager.Version(); err == nil {
		if err := s.processManager.CheckConfig([]byte(candidate)); err != nil {
			c.JSON(400, gin.H{"error": "内核校验失败，原规则未修改：" + err.Error()})
			return
		}
		checked = true
	}
	if err := s.store.UpdateSettings(data.Settings); err != nil {
		c.JSON(500, gin.H{"error": "保存导入规则失败"})
		return
	}
	application, warning := "saved", ""
	if err := s.autoApplyConfig(); err != nil {
		application, warning = "failed", "规则已保存，但自动应用失败，请在配置审阅中检查并应用："+err.Error()
	} else if data.Settings.AutoApply && (data.Settings.DeploymentRole != "gateway" || data.Settings.Gateway.Enabled) && s.processManager.IsRunning() {
		application = "applied"
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"application": application, "warning": warning, "checked": checked})
}
