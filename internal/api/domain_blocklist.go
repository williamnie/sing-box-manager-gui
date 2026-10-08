package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

const domainBlocklistID = "custom-domain-reject"
const maxBlockedDomains = 5000

// 使用现有自定义规则存储，订阅更新不会覆盖用户补充的域名。
func domainBlocklist(rules []storage.Rule) (*storage.Rule, string) {
	var rule *storage.Rule
	for _, r := range rules {
		if r.ID == domainBlocklistID {
			copy := r
			rule = &copy
			break
		}
	}
	raw, _ := json.Marshal(rule)
	return rule, configHash(raw)
}

func plainDomainBlocklist(r *storage.Rule) bool {
	return r == nil || r.RuleType == "domain" && r.Outbound == "REJECT" &&
		len(r.SourceCIDRs)+len(r.Network)+len(r.Protocol)+len(r.Ports)+len(r.PortRanges)+len(r.ProcessNames) == 0
}

func normalizeBlockedDomains(values []string) ([]string, error) {
	if len(values) > maxBlockedDomains {
		return nil, fmt.Errorf("自定义拦截最多保存 %d 个域名", maxBlockedDomains)
	}
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
		valid := len(domain) <= 253 && strings.Contains(domain, ".")
		if _, err := netip.ParseAddr(domain); err == nil {
			valid = false
		}
		for _, label := range strings.Split(domain, ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				valid = false
			}
			for _, char := range label {
				if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
					valid = false
				}
			}
		}
		if !valid {
			return nil, fmt.Errorf("请输入完整域名，不含网址、端口或通配符：%s", value)
		}
		if !seen[domain] {
			result = append(result, domain)
			seen[domain] = true
		}
	}
	return result, nil
}

func (s *Server) domainBlocklistData() gin.H {
	rule, revision := domainBlocklist(s.store.GetRules())
	return gin.H{"rule": rule, "revision": revision, "auto_apply": s.store.GetSettings().AutoApply, "editable": plainDomainBlocklist(rule)}
}

func (s *Server) getDomainBlocklist(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": s.domainBlocklistData()})
}

func (s *Server) addBlockedDomain(c *gin.Context) {
	var req struct {
		Domain string `json:"domain"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "请输入需要拦截的完整域名"})
		return
	}
	domains, err := normalizeBlockedDomains([]string{req.Domain})
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rules := s.store.GetRules()
	rule, _ := domainBlocklist(rules)
	if !plainDomainBlocklist(rule) {
		c.JSON(409, gin.H{"error": "自定义拦截集合的匹配方式已改变，请先在分流规则中检查该规则"})
		return
	}
	if rule != nil {
		if !rule.Enabled {
			c.JSON(409, gin.H{"error": "自定义拦截集合已停用，请先在分流规则中启用，避免意外恢复其他域名的拦截"})
			return
		}
		for _, domain := range rule.Values {
			if strings.EqualFold(strings.TrimSuffix(domain, "."), domains[0]) {
				c.JSON(200, gin.H{"data": s.domainBlocklistData(), "added": false, "application": "saved"})
				return
			}
		}
		domains = append(append([]string{}, rule.Values...), domains...)
	}
	s.saveDomainBlocklist(c, rule, rules, domains, true, true)
}

func (s *Server) updateDomainBlocklist(c *gin.Context) {
	var req struct {
		Domains  *[]string `json:"domains"`
		Revision string    `json:"revision"`
		Enabled  *bool     `json:"enabled"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Domains == nil {
		c.JSON(400, gin.H{"error": "请提供完整域名列表"})
		return
	}
	rules := s.store.GetRules()
	rule, revision := domainBlocklist(rules)
	if revision != req.Revision {
		c.JSON(409, gin.H{"error": "拦截集合已在其他页面更新，请重新载入后再编辑；当前输入仍保留"})
		return
	}
	if !plainDomainBlocklist(rule) {
		c.JSON(409, gin.H{"error": "自定义拦截集合的匹配方式已改变，请在分流规则中编辑原规则"})
		return
	}
	enabled := rule == nil || rule.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	s.saveDomainBlocklist(c, rule, rules, *req.Domains, enabled, false)
}

// 写请求已由 setupRoutes 的 writeMu 串行化，读取、追加和落盘处于同一临界区。
func (s *Server) saveDomainBlocklist(c *gin.Context, rule *storage.Rule, rules []storage.Rule, values []string, enabled, added bool) {
	domains, err := normalizeBlockedDomains(values)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(domains) == 0 {
		if rule != nil {
			err = s.store.DeleteRule(rule.ID)
		}
	} else if rule != nil {
		rule.Values, rule.Enabled = domains, enabled
		err = s.store.UpdateRule(*rule)
	} else {
		priority := 0
		for _, r := range rules {
			if r.Priority <= priority {
				if r.Priority == math.MinInt {
					c.JSON(409, gin.H{"error": "现有规则优先级已达下限，请先调整优先级"})
					return
				}
				priority = r.Priority - 1
			}
		}
		err = s.store.AddRule(storage.Rule{ID: domainBlocklistID, Name: "自定义域名拦截", RuleType: "domain", Values: domains, Outbound: "REJECT", Enabled: enabled, Priority: priority})
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "保存拦截集合失败"})
		return
	}
	application, warning := "saved", ""
	settings := s.store.GetSettings()
	if err = s.autoApplyConfig(); err != nil {
		application, warning = "failed", "拦截集合已保存，但配置应用失败，请到配置审阅检查并应用："+err.Error()
	} else if settings.AutoApply && (settings.DeploymentRole != "gateway" || settings.Gateway.Enabled) && s.processManager.IsRunning() {
		application = "applied"
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": s.domainBlocklistData(), "added": added, "application": application, "warning": warning})
}
