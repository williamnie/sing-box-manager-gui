// Package migration imports source-independent sing-box policies without their runtime setup.
package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

type Report struct {
	Hash     string                  `json:"hash"`
	Summary  map[string]int          `json:"summary"`
	Warnings []string                `json:"warnings"`
	Blockers []string                `json:"blockers"`
	Omitted  []string                `json:"omitted"`
	Policy   *storage.ImportedPolicy `json:"policy"`
}

func Preview(input string) (*Report, error) {
	if len(input) > 3<<20 {
		return nil, fmt.Errorf("配置超过 3 MiB")
	}
	doc, err := configDocument(input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(input))
	r := &Report{Hash: hex.EncodeToString(hash[:]), Summary: map[string]int{}, Warnings: []string{}, Blockers: []string{}, Omitted: []string{}, Policy: &storage.ImportedPolicy{}}
	p := r.Policy
	p.SourceHash = r.Hash
	for key := range doc {
		switch key {
		case "outbounds", "route", "dns":
		case "inbounds", "experimental", "log", "ntp":
			r.Omitted = append(r.Omitted, key+"：使用管理器独立端口、日志和接管配置")
		default:
			r.Blockers = append(r.Blockers, "无法等价导入顶层字段: "+key)
		}
	}
	outs, ok := doc["outbounds"].([]any)
	if !ok || len(outs) == 0 {
		return nil, fmt.Errorf("缺少 outbounds；请提供最终 sing-box JSON 配置。面板数据库不受支持，Clash 订阅请在订阅管理中添加")
	}
	tags := map[string]bool{}
	for _, v := range outs {
		o, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("outbounds 对象无效")
		}
		tag, _ := o["tag"].(string)
		typ, _ := o["type"].(string)
		if tag == "" || tags[tag] {
			return nil, fmt.Errorf("出站标签为空或重复")
		}
		tags[tag] = true
		if typ == "dns" {
			r.Blockers = append(r.Blockers, "旧 DNS 出站需先迁移为 hijack-dns action")
		}
		p.Outbounds = append(p.Outbounds, o)
	}
	// 保留全部选择组、节点协议字段及自定义出站，额外补充管理器必需的别名。
	if !tags["DIRECT"] {
		p.Outbounds = append(p.Outbounds, map[string]any{"type": "direct", "tag": "DIRECT"})
	}
	if !tags["Proxy"] {
		var members []string
		for _, o := range p.Outbounds {
			typ, _ := o["type"].(string)
			if typ != "direct" && typ != "block" && typ != "dns" {
				members = append(members, o["tag"].(string))
			}
		}
		if len(members) == 0 {
			r.Blockers = append(r.Blockers, "没有可供 Proxy 别名使用的代理出站")
		} else {
			p.Outbounds = append(p.Outbounds, map[string]any{"type": "selector", "tag": "Proxy", "outbounds": members, "default": members[0]})
		}
	}
	route, ok := doc["route"].(map[string]any)
	if !ok {
		if doc["route"] != nil {
			return nil, fmt.Errorf("route 必须是对象")
		}
		route = map[string]any{}
	}
	for k := range route {
		switch k {
		case "rules", "rule_set", "final":
		case "auto_detect_interface", "default_interface", "default_domain_resolver", "default_mark", "find_process":
			r.Omitted = append(r.Omitted, "route."+k+"：由新实例管理")
		default:
			r.Blockers = append(r.Blockers, "无法等价导入 route."+k)
		}
	}
	rules, rulesOK := route["rules"].([]any)
	if route["rules"] != nil && !rulesOK {
		return nil, fmt.Errorf("route.rules 必须是数组")
	}
	for i, v := range rules {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("路由规则 %d 无效", i)
		}
		adaptLegacySourceMatch(m, i, r)
		if _, ok := m["inbound"]; ok {
			r.Blockers = append(r.Blockers, fmt.Sprintf("规则 %d 依赖旧实例 inbound 标签，需要人工改写", i+1))
		}
		if _, ok := m["process_name"]; ok {
			r.Warnings = append(r.Warnings, fmt.Sprintf("规则 %d 进程匹配仅适用于本机，网关模式将拒绝", i+1))
		}
		if m["outbound"] == "DIRECT" && m["source_ip_cidr"] == nil && (m["port"] != nil || m["port_range"] != nil) {
			r.Warnings = append(r.Warnings, fmt.Sprintf("规则 %d 是不限来源的端口直连，可能包含 STUN/19302；请限制到 BT 专属来源，监听端口不能代表所有对端端口", i+1))
		}
		p.Rules = append(p.Rules, m)
	}
	sets, setsOK := route["rule_set"].([]any)
	if route["rule_set"] != nil && !setsOK {
		return nil, fmt.Errorf("route.rule_set 必须是数组")
	}
	for _, v := range sets {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rule_set 对象无效")
		}
		if m["type"] == "local" {
			r.Blockers = append(r.Blockers, "本地 rule_set 文件无法随 JSON 等价迁移，请改为内联或远程规则集")
		}
		p.RuleSets = append(p.RuleSets, m)
	}
	p.Final, _ = route["final"].(string)
	if p.Final == "" {
		p.Final = p.Outbounds[0]["tag"].(string)
	}
	if doc["dns"] != nil {
		if _, ok := doc["dns"].(map[string]any); !ok {
			return nil, fmt.Errorf("dns 必须是对象")
		}
	}
	if dns, ok := doc["dns"].(map[string]any); ok {
		p.DNS = dns
		servers, serversOK := dns["servers"].([]any)
		if dns["servers"] != nil && !serversOK {
			return nil, fmt.Errorf("dns.servers 必须是数组")
		}
		if dns["rules"] != nil {
			if _, ok := dns["rules"].([]any); !ok {
				return nil, fmt.Errorf("dns.rules 必须是数组")
			}
		}
		for _, v := range servers {
			m, ok := v.(map[string]any)
			if !ok {
				r.Blockers = append(r.Blockers, "DNS server 格式无效")
				continue
			}
			if m["type"] == "hosts" && m["path"] != nil {
				r.Blockers = append(r.Blockers, "hosts.path 依赖旧宿主文件；请导出 predefined 映射")
			}
			if m["type"] == nil {
				r.Blockers = append(r.Blockers, "旧 address DNS 格式不兼容现代内核，请先转换为 typed DNS")
			}
		}
	}
	// 不从导入配置继承本机文件访问或策略标记，避免越过受控运行目录。
	var scan func(any, string)
	scan = func(v any, path string) {
		switch m := v.(type) {
		case map[string]any:
			for k, x := range m {
				if k == "inbound" || k == "source_ips" {
					r.Blockers = append(r.Blockers, "嵌套规则依赖旧匹配字段: "+path+"."+k)
				}
				if k == "certificate_path" || k == "key_path" || k == "private_key_path" || k == "routing_mark" || k == "netns" {
					r.Blockers = append(r.Blockers, "需人工迁移的宿主依赖: "+path+"."+k)
				}
				if k == "path" {
					if _, ok := x.([]any); ok {
						r.Blockers = append(r.Blockers, "不导入宿主文件列表: "+path+".path")
					}
				}
				scan(x, path+"."+k)
			}
		case []any:
			for _, x := range m {
				scan(x, path)
			}
		}
	}
	scan(p.Outbounds, "outbounds")
	scan(p.DNS, "dns")
	scan(p.Rules, "route.rules")
	// []map 与 []any 在运行时不同，显式扫描各对象。
	for _, o := range p.Outbounds {
		scan(o, "outbound")
	}
	for _, v := range p.Rules {
		scan(v, "rule")
	}
	renameDNS(p)
	r.Summary["outbounds"] = len(p.Outbounds)
	r.Summary["rules"] = len(p.Rules)
	r.Summary["rule_sets"] = len(p.RuleSets)
	r.Warnings = append(r.Warnings, "导入只保存草案，关闭自动应用和网关接管；并行验证使用独立端口且不启用 TUN。旧实例必须停止接管后才能切换。")
	// map 的迭代次序不稳定；同一输入应产生相同的预览报告。
	sort.Strings(r.Omitted)
	sort.Strings(r.Blockers)
	return r, nil
}

func configDocument(input string) (map[string]any, error) {
	var doc map[string]any
	if json.Unmarshal([]byte(input), &doc) != nil || doc == nil {
		return nil, fmt.Errorf("请提供有效的 sing-box JSON 配置对象")
	}
	wrapped, exists := doc["config"]
	if !exists {
		return doc, nil
	}
	// 兼容已有导出包装；核心策略只依赖最终 sing-box 配置，不依赖来源面板。
	switch value := wrapped.(type) {
	case map[string]any:
		return value, nil
	case string:
		var config map[string]any
		if json.Unmarshal([]byte(value), &config) == nil && config != nil {
			return config, nil
		}
	}
	return nil, fmt.Errorf("config 必须是 sing-box JSON 对象或包含该对象的 JSON 字符串")
}

// 个别旧导出使用 source_ips；适配不改变节点标签、引用或路由次序。
func adaptLegacySourceMatch(rule map[string]any, index int, report *Report) {
	sources, exists := rule["source_ips"]
	if !exists {
		return
	}
	if _, exists := rule["source_ip_cidr"]; exists {
		report.Blockers = append(report.Blockers, "规则同时含 source_ips/source_ip_cidr")
		return
	}
	rule["source_ip_cidr"] = sources
	delete(rule, "source_ips")
	report.Warnings = append(report.Warnings, fmt.Sprintf("规则 %d 的 source_ips 转为 source_ip_cidr", index+1))
}

func renameDNS(p *storage.ImportedPolicy) {
	if p.DNS == nil {
		return
	}
	mapping := map[string]string{}
	servers, _ := p.DNS["servers"].([]any)
	for _, v := range servers {
		if m, ok := v.(map[string]any); ok {
			tag, _ := m["tag"].(string)
			if strings.HasPrefix(tag, "dns_") {
				mapping[tag] = "imported-" + tag
				m["tag"] = mapping[tag]
			}
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch m := v.(type) {
		case map[string]any:
			for k, x := range m {
				if k == "server" || k == "final" || k == "domain_resolver" {
					if s, ok := x.(string); ok && mapping[s] != "" {
						m[k] = mapping[s]
					}
				}
				walk(x)
			}
		case []any:
			for _, x := range m {
				walk(x)
			}
		}
	}
	walk(p.DNS)
	for _, r := range p.Rules {
		walk(r)
	}
	for _, o := range p.Outbounds {
		walk(o)
	}
}
