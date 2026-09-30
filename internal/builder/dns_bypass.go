package builder

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/gateway"
)

func (b *ConfigBuilder) dnsBypass() bool {
	return b.settings.DeploymentRole == "gateway" && gateway.Normalize(b.settings.Gateway).AccessMode == "dns"
}

// configureDNSBypass 投影 DNS 能判断的条件；流量端口、嗅探协议和目的 IP 绝不剥离。
// FakeIP 映射只保存域名身份，出站仍在每次连接按原始来源和完整路由规则选择。
func (b *ConfigBuilder) configureDNSBypass(c *SingBoxConfig) error {
	g := gateway.Normalize(b.settings.Gateway)
	if g.DNSSource == "router" {
		for _, r := range c.Route.Rules {
			if containsSource(r) {
				return fmt.Errorf("主路由转发 DNS 无法保留客户端来源，请移除来源规则或选择 DHCP 直接下发 DNS")
			}
		}
	}
	servers := []any{}
	for _, s := range c.DNS.Servers {
		if s.Tag != "dns_fakeip" && s.Tag != "dns_proxy" {
			servers = append(servers, s)
		}
	}
	servers = append(servers, DNSServer{Type: "fakeip", Tag: "dns_fakeip", Inet4Range: g.FakeIPRange})
	rules := []any{}
	// 用户 hosts 永远优先，且内部代理节点解析不能得到 FakeIP。
	for _, r := range c.DNS.Rules {
		if r.Server == "dns_hosts" {
			rules = append(rules, r)
		}
	}
	imported, importedServers, err := b.importedDNSBypass()
	if err != nil {
		return err
	}
	if len(imported) > 0 && len(imported)*(len(c.Route.Rules)+1) > 10000 {
		return fmt.Errorf("导入 DNS 与流量规则的组合超过 10000 项，请精简后再启用 DNS 旁路")
	}
	servers = append(servers, importedServers...)
	rules = append(rules, map[string]any{"inbound": []string{"lan-dns"}, "invert": true, "action": "route", "server": "dns_direct"})
	for _, d := range b.settings.Devices {
		if d.Enabled {
			group := b.group(d.GroupID)
			if group.Policy == "direct" || group.Policy == "bypass" {
				rules = append(rules, dnsActions(map[string]any{"source_ip_cidr": d.Addresses}, "direct", "dns_direct")...)
			}
		}
	}
	for _, r := range b.settings.SplitDNS {
		match := map[string]any{"domain_suffix": r.DomainSuffix}
		if len(r.SourceCIDRs) > 0 {
			match["source_ip_cidr"] = r.SourceCIDRs
		}
		rules = append(rules, dnsActions(match, r.Server, "dns_"+r.Server)...)
	}
	// 先按业务流量选分支，再在该分支内按导入 DNS 次序选解析器。
	// DNS 上游的 transport/detour 不能用来推断业务连接应直连还是代理。
	managedCount := len(c.Route.Rules)
	if p := b.settings.ImportedPolicy; p != nil {
		managedCount -= len(p.Rules)
	}
	for i, r := range c.Route.Rules {
		match, ok := b.dnsMatch(r)
		if !ok {
			continue
		}
		out, _ := r["outbound"].(string)
		kind := b.dnsOutboundKind(c, out, map[string]bool{})
		// 目标改写必须让连接进入实例才能执行，即使出站是 DIRECT。
		if r["override_address"] != nil || r["override_port"] != nil {
			kind = "proxy"
		}
		if r["action"] == "reject" {
			kind = "reject"
		}
		if out == "" && kind != "reject" {
			continue
		}
		if i >= managedCount {
			rules = append(rules, importedDNSActions(match, kind, imported)...)
		} else {
			rules = append(rules, dnsActions(match, kind, "dns_direct")...)
		}
	}
	rules = append(rules, importedDNSActions(map[string]any{}, b.dnsOutboundKind(c, c.Route.Final, map[string]bool{}), imported)...)
	c.DNS.Raw = map[string]any{"servers": servers, "rules": rules, "final": "dns_direct", "disable_cache": true, "independent_cache": true}
	if c.Experimental == nil {
		c.Experimental = &ExperimentalConfig{}
	}
	// 同一地址池跨策略更新保留映射；改池独立文件，避免旧映射指向不同域名。
	hash := sha256.Sum256([]byte(g.FakeIPRange))
	c.Experimental.CacheFile = &CacheFileConfig{Enabled: true, Path: fmt.Sprintf("dns-bypass-%x.db", hash[:8]), StoreFakeIP: true}
	return nil
}

func containsSource(v any) bool {
	switch m := v.(type) {
	case RouteRule:
		return containsSource(map[string]any(m))
	case map[string]any:
		for k, v := range m {
			if strings.HasPrefix(k, "source_") || containsSource(v) {
				return true
			}
		}
	case []any:
		for _, v := range m {
			if containsSource(v) {
				return true
			}
		}
	}
	return false
}

func dnsActions(match map[string]any, kind, realServer string) []any {
	if kind == "direct" || kind == "hosts" {
		r := deepMap(match)
		r["action"] = "route"
		r["server"] = realServer
		return []any{r}
	}
	if kind == "reject" {
		r := deepMap(match)
		r["action"] = "reject"
		r["no_drop"] = true
		return []any{r}
	}
	// IPv4-only：仅 A 返回 FakeIP，其余查询空答，避免 ANY/HTTPS/SVCB 以及
	// MX/SRV 等附加区携带可绕过旁路的真实地址。直连和 hosts 不受此限制。
	var rules []any
	for _, a := range []map[string]any{
		{"query_type": []string{"A"}, "action": "route", "server": "dns_fakeip", "rewrite_ttl": 60},
		{"action": "predefined", "rcode": "NOERROR"},
	} {
		r := deepMap(match)
		// 导入 query_type 与生成条件必须求交，不能覆盖原条件扩大范围。
		if a["query_type"] != nil && (r["query_type"] != nil || r["invert"] == true || r["type"] == "logical") {
			r = map[string]any{"type": "logical", "mode": "and", "rules": []any{r, map[string]any{"query_type": a["query_type"]}}}
			for k, v := range a {
				if k != "query_type" {
					r[k] = v
				}
			}
		} else {
			for k, v := range a {
				r[k] = v
			}
		}
		rules = append(rules, r)
	}
	return rules
}

func (b *ConfigBuilder) dnsMatch(r map[string]any) (map[string]any, bool) {
	m := map[string]any{}
	for k, v := range r {
		switch k {
		case "outbound", "action", "override_address", "override_port":
		case "domain", "domain_suffix", "domain_keyword", "domain_regex", "source_ip_cidr", "invert", "query_type":
			m[k] = v
		case "type", "mode":
			m[k] = v
		case "rules":
			children, ok := v.([]any)
			if !ok {
				return nil, false
			}
			var safe []any
			for _, x := range children {
				child, ok := x.(map[string]any)
				if !ok {
					return nil, false
				}
				match, ok := b.dnsMatch(child)
				if !ok {
					return nil, false
				}
				safe = append(safe, match)
			}
			m[k] = safe
		case "rule_set":
			for _, tag := range stringsFrom(v) {
				if !b.domainRuleSet(tag) {
					return nil, false
				}
			}
			m[k] = v
		default:
			return nil, false
		}
	}
	// 空条件的 route/reject 是合法的兜底规则，调用方再判断是否为终结动作。
	return m, true
}

func (b *ConfigBuilder) domainRuleSet(tag string) bool {
	// 只信任管理器已知的 geosite 语义；导入远端二进制规则集不能只凭标签猜测。
	for _, group := range b.ruleGroups {
		if group.Enabled {
			for _, site := range group.SiteRules {
				if tag == "geosite-"+site {
					return true
				}
			}
		}
	}
	for _, r := range b.rules {
		if r.Enabled && r.RuleType == "geosite" {
			for _, site := range r.Values {
				if tag == "geosite-"+site {
					return true
				}
			}
		}
	}
	if p := b.settings.ImportedPolicy; p != nil {
		for _, set := range p.RuleSets {
			if set["tag"] != tag || set["type"] != "inline" {
				continue
			}
			items, ok := set["rules"].([]any)
			if !ok || len(items) == 0 {
				return false
			}
			for _, item := range items {
				m, ok := item.(map[string]any)
				if !ok {
					return false
				}
				for k := range m {
					if k != "domain" && k != "domain_suffix" && k != "domain_keyword" && k != "domain_regex" {
						return false
					}
				}
			}
			return true
		}
	}
	return false
}

// 混有代理和直连的动态选择组保守返回 FakeIP，连接时仍可选择 DIRECT。
// 明确的直连预设在 buildRoute 中使用配置的出站，避免控制面板临时选择造成 DNS/路由不一致。
func (b *ConfigBuilder) dnsOutboundKind(c *SingBoxConfig, tag string, visiting map[string]bool) string {
	if tag == "REJECT" {
		return "reject"
	}
	if visiting[tag] {
		return "proxy"
	}
	visiting[tag] = true
	defer delete(visiting, tag)
	for _, o := range c.Outbounds {
		if o["tag"] != tag {
			continue
		}
		if o["type"] == "direct" && o["detour"] == nil {
			return "direct"
		}
		if o["type"] == "block" {
			return "reject"
		}
		if refs := stringsFrom(o["outbounds"]); len(refs) > 0 {
			kind := b.dnsOutboundKind(c, refs[0], visiting)
			for _, ref := range refs[1:] {
				if b.dnsOutboundKind(c, ref, visiting) != kind {
					return "proxy"
				}
			}
			return kind
		}
	}
	return "proxy"
}

type importedDNSRule struct {
	match        map[string]any
	kind, server string
	options      map[string]any
}

func importedDNSActions(match map[string]any, kind string, imported []importedDNSRule) []any {
	if kind == "reject" {
		return dnsActions(match, kind, "")
	}
	var rules []any
	for _, r := range imported {
		combined := r.match
		if len(match) > 0 {
			combined = match
			if len(r.match) > 0 {
				combined = map[string]any{"type": "logical", "mode": "and", "rules": []any{match, r.match}}
			}
		}
		resultKind := kind
		switch r.kind {
		case "hosts":
			resultKind = "direct"
		case "fakeip":
			resultKind = "proxy"
		case "reject":
			resultKind = "reject"
		}
		actions := dnsActions(combined, resultKind, r.server)
		if resultKind == "direct" {
			for _, action := range actions {
				for key, value := range r.options {
					action.(map[string]any)[key] = value
				}
			}
		}
		rules = append(rules, actions...)
	}
	return append(rules, dnsActions(match, kind, "dns_direct")...)
}

func (b *ConfigBuilder) importedDNSBypass() (rules []importedDNSRule, servers []any, err error) {
	p := b.settings.ImportedPolicy
	if p == nil || p.DNS == nil {
		return
	}
	dns := deepMap(p.DNS)
	byTag := map[string]map[string]any{}
	items, _ := dns["servers"].([]any)
	for _, item := range items {
		s, ok := item.(map[string]any)
		if !ok {
			err = fmt.Errorf("导入 DNS 服务器无效")
			return
		}
		tag, _ := s["tag"].(string)
		if tag == "" || strings.HasPrefix(tag, "dns_") {
			err = fmt.Errorf("导入 DNS 标签为空或占用管理器保留标签: %s", tag)
			return
		}
		byTag[tag] = s
		if s["type"] == "fakeip" {
			continue
		}
		servers = append(servers, s)
		if s["type"] == "hosts" {
			if s["path"] != nil {
				err = fmt.Errorf("DNS 旁路不支持导入 hosts 文件路径，请使用 hosts 映射")
				return
			}
		}
	}
	items, _ = dns["rules"].([]any)
	for i, item := range items {
		r, ok := item.(map[string]any)
		if !ok {
			err = fmt.Errorf("导入 DNS 规则无效")
			return
		}
		if gateway.Normalize(b.settings.Gateway).DNSSource == "router" && containsSource(r) {
			err = fmt.Errorf("主路由转发 DNS 不能使用导入 DNS 来源规则")
			return
		}
		action, _ := r["action"].(string)
		tag, _ := r["server"].(string)
		match := deepMap(r)
		for _, k := range []string{"server", "action", "disable_cache", "rewrite_ttl", "strategy"} {
			delete(match, k)
		}
		safe, valid := b.dnsMatch(match)
		if len(match) == 0 {
			safe, valid = map[string]any{}, true
		}
		if !valid || (action != "" && action != "route" && action != "reject") {
			err = fmt.Errorf("导入 DNS 规则 %d 不能安全转换为 DNS 分流，请改用 hosts/Split DNS/域名规则（原数据保留）", i+1)
			return
		}
		if action == "reject" {
			rules = append(rules, importedDNSRule{match: safe, kind: "reject"})
			continue
		}
		server := byTag[tag]
		if server == nil {
			err = fmt.Errorf("导入 DNS 规则引用未知服务器 %s", tag)
			return
		}
		kind := "resolver"
		if server["type"] == "fakeip" {
			kind = "fakeip"
		} else if server["type"] == "hosts" {
			kind = "hosts"
		}
		options := map[string]any{}
		for _, key := range []string{"strategy", "rewrite_ttl"} {
			if value, ok := r[key]; ok {
				options[key] = value
			}
		}
		rules = append(rules, importedDNSRule{match: safe, kind: kind, server: tag, options: options})
	}
	if tag, _ := dns["final"].(string); tag != "" {
		server := byTag[tag]
		if server == nil {
			err = fmt.Errorf("导入 DNS 默认解析器不存在: %s", tag)
			return
		}
		kind := "resolver"
		if server["type"] == "fakeip" {
			kind = "fakeip"
		} else if server["type"] == "hosts" {
			kind = "hosts"
		}
		rules = append(rules, importedDNSRule{match: map[string]any{}, kind: kind, server: tag})
	}
	return
}

// DNSBypassWarnings 用于预览，说明无法投影的规则仍保留为进入实例后的流量规则。
func DNSBypassWarnings() string {
	return "DNS 旁路只覆盖使用本 DNS 且被 FakeIP 静态路由送达的连接；端口、协议、目的 IP 及不透明导入规则集只在流量进入实例后匹配。代理域名只对 A 返回 FakeIP，其余类型（含 AAAA/HTTPS/SVCB/ANY/TXT/MX）空答；需要非 A 记录的域名请配置直连或 Split DNS。动态混合选择组返回 FakeIP；直连预设按已保存出站生成，修改后需重新应用。导入 DNS 的默认解析器按流量最终出站重建，原始导入数据保留。"
}
