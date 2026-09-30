package builder

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/gateway"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// WithPlatform 用于离线预览与隔离测试；真正执行时另由特权接口检查宿主系统。
func (b *ConfigBuilder) WithPlatform(platform string) *ConfigBuilder { b.platform = platform; return b }
func (b *ConfigBuilder) validate() error {
	if b.settings == nil {
		return fmt.Errorf("缺少设置")
	}
	if err := storage.ValidateSettings(b.settings); err != nil {
		return err
	}
	if b.settings.DeploymentRole == "gateway" {
		if p := b.settings.ImportedPolicy; p != nil {
			var check func(any) bool
			check = func(v any) bool {
				switch m := v.(type) {
				case map[string]any:
					for k, x := range m {
						if strings.HasPrefix(k, "process_") || k == "user" || k == "user_id" || k == "package_name" {
							return true
						}
						if check(x) {
							return true
						}
					}
				case []any:
					for _, x := range m {
						if check(x) {
							return true
						}
					}
				}
				return false
			}
			for _, r := range p.Rules {
				if check(r) {
					return fmt.Errorf("导入规则依赖远端进程/用户匹配，网关无法识别")
				}
			}
		}
		if p := b.settings.ImportedPolicy; p != nil && p.DNS != nil {
			encoded, _ := json.Marshal(p.DNS)
			for _, key := range []string{"process_name", "process_path", "process_path_regex", "user_id", "package_name"} {
				if strings.Contains(string(encoded), `"`+key+`"`) {
					return fmt.Errorf("导入 DNS 含无法识别的远端进程条件")
				}
			}
		}
		if b.platform != "linux" {
			return fmt.Errorf("家庭网关只支持 Linux；macOS 请使用单机模式")
		}
		if !b.profile.Known || b.profile.Major != 1 || b.profile.Minor < 14 {
			return fmt.Errorf("家庭网关要求已识别的 sing-box 1.14 或以上 1.x 内核")
		}
		if err := gateway.Validate(gateway.Normalize(b.settings.Gateway)); err != nil {
			return err
		}
	}
	if _, err := parseDNSServer(b.settings.ProxyDNS, "dns_proxy", "Proxy"); err != nil {
		return err
	}
	if _, err := parseDNSServer(b.settings.DirectDNS, "dns_direct", ""); err != nil {
		return err
	}
	for _, r := range b.rules {
		if r.Enabled {
			if err := storage.ValidateRule(r, b.settings.DeploymentRole == "gateway"); err != nil {
				return fmt.Errorf("规则 %s: %w", r.Name, err)
			}
		}
	}
	return nil
}
func parseDNSServer(value, tag, detour string) (DNSServer, error) {
	server := DNSServer{Tag: tag, Detour: detour}
	if !strings.Contains(value, "://") {
		value = "udp://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return server, fmt.Errorf("DNS 地址格式无效")
	}
	switch u.Scheme {
	case "udp", "tcp", "tls", "https", "quic", "h3":
	default:
		return server, fmt.Errorf("不支持的 DNS 协议")
	}
	if u.Scheme != "https" && u.Scheme != "h3" && u.Path != "" {
		return server, fmt.Errorf("该 DNS 协议不支持路径")
	}
	server.Type = u.Scheme
	server.Server = u.Hostname()
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return server, fmt.Errorf("DNS 端口无效")
		}
		server.ServerPort = p
	}
	if u.Scheme == "https" || u.Scheme == "h3" {
		server.Path = u.EscapedPath()
	}
	if net.ParseIP(server.Server) == nil {
		server.DomainResolver = "dns_bootstrap"
	}
	return server, nil
}
func (b *ConfigBuilder) systemHosts() map[string][]string {
	if b.settings.DeploymentRole == "gateway" {
		return map[string][]string{}
	}
	return ParseSystemHosts()
}
func ruleMatch(r storage.Rule) RouteRule {
	m := RouteRule{}
	if len(r.SourceCIDRs) > 0 {
		m["source_ip_cidr"] = r.SourceCIDRs
	}
	if len(r.Network) > 0 {
		m["network"] = r.Network
	}
	if len(r.Protocol) > 0 {
		m["protocol"] = r.Protocol
	}
	if len(r.Ports) > 0 {
		m["port"] = r.Ports
	}
	if len(r.PortRanges) > 0 {
		m["port_range"] = r.PortRanges
	}
	if len(r.ProcessNames) > 0 {
		m["process_name"] = r.ProcessNames
	}
	return m
}
func (b *ConfigBuilder) group(id string) storage.DeviceGroup {
	for _, g := range b.settings.DeviceGroups {
		if g.ID == id {
			return g
		}
	}
	return storage.DeviceGroup{Policy: "split"}
}
func (b *ConfigBuilder) deviceRouteRules() []RouteRule {
	var rules []RouteRule
	for _, d := range storage.EffectiveDevices(b.settings) {
		if !d.Enabled {
			continue
		}
		g := b.group(d.GroupID)
		r := RouteRule{"source_ip_cidr": d.Addresses}
		switch g.Policy {
		case "direct":
			r["outbound"] = "DIRECT"
		case "bypass":
			if !b.dnsBypass() {
				r["action"] = "bypass"
			}
			r["outbound"] = "DIRECT"
		case "strict":
			out := g.Outbound
			if out == "" {
				out = "Proxy"
			}
			r["outbound"] = out
		default:
			continue
		}
		rules = append(rules, r)
	}
	return rules
}
func (b *ConfigBuilder) policyDNSRules() []DNSRule {
	rules := []DNSRule{}
	if b.settings.DeploymentRole == "gateway" {
		for _, d := range storage.EffectiveDevices(b.settings) {
			if !d.Enabled {
				continue
			}
			g := b.group(d.GroupID)
			server := ""
			switch g.Policy {
			case "direct", "bypass":
				server = "dns_direct"
			case "strict":
				server = "dns_strict_" + g.ID
			}
			if server != "" {
				rules = append(rules, DNSRule{SourceCIDRs: d.Addresses, Server: server, Action: "route"})
			}
		}
	}
	for _, r := range b.settings.SplitDNS {
		rules = append(rules, DNSRule{SourceCIDRs: r.SourceCIDRs, DomainSuffix: r.DomainSuffix, Server: "dns_" + r.Server, Action: "route"})
	}
	// 只把 DNS 可判断的域名条件映射进 DNS，端口/流量协议规则不得过度扩大。
	sorted := append([]storage.Rule{}, b.rules...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })
	for _, r := range sorted {
		if !r.Enabled || len(r.Network)+len(r.Protocol)+len(r.Ports)+len(r.PortRanges)+len(r.ProcessNames) > 0 {
			continue
		}
		if r.Outbound != "DIRECT" && r.Outbound != "Proxy" && r.Outbound != "REJECT" {
			continue
		}
		dr := DNSRule{SourceCIDRs: r.SourceCIDRs, Action: "route", Server: "dns_proxy"}
		if r.Outbound == "DIRECT" {
			dr.Server = "dns_direct"
		}
		if r.Outbound == "REJECT" {
			dr.Server = ""
			dr.Action = "reject"
		}
		switch r.RuleType {
		case "domain":
			dr.Domain = r.Values
		case "domain_suffix":
			dr.DomainSuffix = r.Values
		case "domain_keyword":
			dr.DomainKeyword = r.Values
		case "geosite":
			for _, v := range r.Values {
				dr.RuleSet = append(dr.RuleSet, "geosite-"+v)
			}
		default:
			continue
		}
		rules = append(rules, dr)
	}
	return rules
}
func stringsFrom(v any) []string {
	switch a := v.(type) {
	case []string:
		return a
	case string:
		return []string{a}
	case []any:
		var r []string
		for _, s := range a {
			if t, ok := s.(string); ok {
				r = append(r, t)
			}
		}
		return r
	}
	return nil
}
func (b *ConfigBuilder) validateOutbounds(c *SingBoxConfig) error {
	outs := map[string]Outbound{}
	for _, o := range c.Outbounds {
		tag, _ := o["tag"].(string)
		if tag == "" || outs[tag] != nil {
			return fmt.Errorf("出站标签为空或重复: %s", tag)
		}
		outs[tag] = o
	}
	if direct := outs["DIRECT"]; direct == nil || direct["type"] != "direct" {
		return fmt.Errorf("DIRECT 必须是直连出站")
	}
	var visit func(string, map[string]bool, bool) error
	visit = func(tag string, path map[string]bool, strict bool) error {
		if path[tag] {
			return fmt.Errorf("出站引用循环: %s", tag)
		}
		o := outs[tag]
		if o == nil {
			return fmt.Errorf("出站不存在: %s", tag)
		}
		if strict && o["type"] == "direct" {
			return fmt.Errorf("严格代理的出站 %s 含直连路径", tag)
		}
		path[tag] = true
		defer delete(path, tag)
		refs := stringsFrom(o["outbounds"])
		if detour, _ := o["detour"].(string); detour != "" {
			refs = append(refs, detour)
		}
		for _, ref := range refs {
			if err := visit(ref, path, strict); err != nil {
				return err
			}
		}
		if def, _ := o["default"].(string); def != "" {
			found := false
			for _, ref := range refs {
				if ref == def {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("选择组 %s 的默认出站不在成员中", tag)
			}
		}
		return nil
	}
	for tag := range outs {
		if err := visit(tag, map[string]bool{}, false); err != nil {
			return err
		}
	}
	if err := visit(c.Route.Final, map[string]bool{}, false); err != nil {
		return err
	}
	for _, r := range c.Route.Rules {
		if out, _ := r["outbound"].(string); out != "" {
			if err := visit(out, map[string]bool{}, false); err != nil {
				return err
			}
		}
	}
	if b.settings.DeploymentRole == "gateway" {
		for _, g := range b.settings.DeviceGroups {
			if g.Policy == "strict" {
				tag := g.Outbound
				if tag == "" {
					tag = "Proxy"
				}
				if err := visit(tag, map[string]bool{}, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// 保留导入对象的未知字段，使受控自定义出站和规则不会在序列化中静默损失。
func (b *ConfigBuilder) applyImported(c *SingBoxConfig) error {
	p := b.settings.ImportedPolicy
	if p == nil {
		return nil
	}
	generated := c.Outbounds
	c.Outbounds = nil
	tags := map[string]bool{}
	for _, o := range p.Outbounds {
		tag, _ := o["tag"].(string)
		tags[tag] = true
		c.Outbounds = append(c.Outbounds, Outbound(deepMap(o)))
	}
	rename := map[string]string{"Auto": "Managed Auto", "Proxy": "Managed Proxy", "Final": "Managed Final"}
	for _, o := range generated {
		original, _ := o["tag"].(string)
		if original == "DIRECT" || original == "REJECT" {
			if !tags[original] {
				c.Outbounds = append(c.Outbounds, o)
				tags[original] = true
			}
			continue
		}
		if len(b.nodes) == 0 {
			continue
		}
		item := Outbound(deepMap(o))
		tag := original
		if mapped := rename[tag]; mapped != "" {
			tag = mapped
			item["tag"] = tag
		}
		// 仅重命名管理器生成的选择组引用，不改变导入选择组和节点的成员。
		if item["type"] == "selector" || item["type"] == "urltest" {
			refs := stringsFrom(item["outbounds"])
			for i, v := range refs {
				if mapped := rename[v]; mapped != "" {
					refs[i] = mapped
				}
			}
			item["outbounds"] = refs
			if def, ok := item["default"].(string); ok && rename[def] != "" {
				item["default"] = rename[def]
			}
		}
		if tags[tag] {
			return fmt.Errorf("新增节点或选择组与导入标签冲突: %s，请重命名", tag)
		}
		tags[tag] = true
		c.Outbounds = append(c.Outbounds, item)
	}

	// 所有管理器策略先于导入规则；保留导入规则的原始次序。
	prefix := c.Route.Rules
	for _, r := range p.Rules {
		prefix = append(prefix, RouteRule(r))
	}
	c.Route.Rules = prefix
	c.Route.Final = p.Final
	for _, r := range p.RuleSets {
		c.Route.RuleSet = append(c.Route.RuleSet, RuleSet{Raw: r})
	}
	if p.DNS != nil && !b.dnsBypass() {
		raw := deepMap(p.DNS)
		raw["independent_cache"] = true
		servers, _ := raw["servers"].([]any)
		existing := map[string]bool{}
		for _, v := range servers {
			if m, ok := v.(map[string]any); ok {
				tag, _ := m["tag"].(string)
				existing[tag] = true
			}
		}
		for _, s := range c.DNS.Servers {
			if existing[s.Tag] {
				return fmt.Errorf("导入 DNS 保留标签冲突: %s", s.Tag)
			}
			if s.Tag == "dns_fakeip" {
				continue
			}
			bytes, _ := json.Marshal(s)
			var m any
			_ = json.Unmarshal(bytes, &m)
			servers = append(servers, m)
		}
		raw["servers"] = servers
		var rules []any
		// 管理器 hosts 和来源策略先执行，保留旧 DNS 的相对次序。
		for _, r := range c.DNS.Rules {
			if r.Server != "dns_fakeip" {
				bytes, _ := json.Marshal(r)
				var v any
				_ = json.Unmarshal(bytes, &v)
				rules = append(rules, v)
			}
		}
		prior, _ := raw["rules"].([]any)
		rules = append(rules, prior...)
		raw["rules"] = rules
		c.DNS.Raw = raw
	}
	return nil
}
func deepMap(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var r map[string]any
	_ = json.Unmarshal(b, &r)
	return r
}
func (d DNSConfig) MarshalJSON() ([]byte, error) {
	if d.Raw != nil {
		return json.Marshal(d.Raw)
	}
	type plain DNSConfig
	return json.Marshal(plain(d))
}
func (r RuleSet) MarshalJSON() ([]byte, error) {
	if r.Raw != nil {
		return json.Marshal(r.Raw)
	}
	type plain RuleSet
	return json.Marshal(plain(r))
}
