package builder

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 整理仅作用于构建副本，撤销方案即可恢复原始导入组，不覆盖来源规则或节点。
func (b *ConfigBuilder) prepareProxyPlan() (*ConfigBuilder, error) {
	plan := b.settings.ProxyPlan
	primary := plan.Primary
	found := primary == "Proxy"
	for _, f := range b.filters {
		if f.Enabled && f.Name == primary {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("默认代理必须选择一个启用的过滤器")
	}
	prepared := *b
	raw, err := json.Marshal(b.settings)
	if err != nil {
		return nil, err
	}
	var settings storage.Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	prepared.settings, prepared.proxyPrepared = &settings, true
	prepared.rules = slices.Clone(b.rules)
	prepared.ruleGroups = slices.Clone(b.ruleGroups)
	aliases := map[string]string{}
	imported := map[string]map[string]any{}
	if p := settings.ImportedPolicy; p != nil {
		for _, o := range p.Outbounds {
			tag, _ := o["tag"].(string)
			imported[tag] = o
		}
	}
	prefix := ""
	if settings.ImportedPolicy != nil {
		prefix = "Managed "
	}
	for _, name := range []string{"Auto", "Proxy", "Final"} {
		tag := prefix + name
		if primary == tag && primary != "Proxy" {
			return nil, fmt.Errorf("默认代理名称与系统分组 %s 冲突", tag)
		}
		if imported[tag] == nil {
			aliases[tag] = primary
		}
	}
	// 无导入 Proxy 时，内部 DNS 与设备策略的历史入口也使用统一入口。
	if imported["Proxy"] == nil {
		aliases["Proxy"] = primary
	}
	for _, node := range b.nodes {
		country := node.Country
		if country == "" {
			country = "OTHER"
		}
		tag := fmt.Sprintf("%s %s", storage.GetCountryEmoji(country), storage.GetCountryName(country))
		if !plan.ManagedOnly && tag != primary && imported[tag] == nil {
			aliases[tag] = primary
		}
	}
	merged := map[string]bool{}
	for _, tag := range plan.MergeGroups {
		o := imported[tag]
		if o == nil || (o["type"] != "selector" && o["type"] != "urltest") || tag == primary {
			return nil, fmt.Errorf("只能合并现有的导入选择组或自动测速组: %s", tag)
		}
		aliases[tag], merged[tag] = primary, true
	}
	// 预设分类规则直接指向其动作，不再为直连/拦截或每个分类额外生成选择组。
	for _, g := range b.ruleGroups {
		if g.Enabled && g.Name != primary && imported[g.Name] == nil {
			aliases[g.Name] = g.Outbound
		}
	}
	resolve := func(tag string) string {
		seen := map[string]bool{}
		for aliases[tag] != "" && !seen[tag] {
			seen[tag] = true
			tag = aliases[tag]
		}
		return tag
	}
	for i := range prepared.rules {
		prepared.rules[i].Outbound = resolve(prepared.rules[i].Outbound)
	}
	for i := range prepared.ruleGroups {
		prepared.ruleGroups[i].Outbound = resolve(prepared.ruleGroups[i].Outbound)
	}
	for i := range settings.DeviceGroups {
		group := &settings.DeviceGroups[i]
		if group.Policy == "strict" && group.Outbound == "" {
			group.Outbound = primary
		} else {
			group.Outbound = resolve(group.Outbound)
		}
	}
	settings.FinalOutbound = primary
	if p := settings.ImportedPolicy; p != nil {
		p.Final = primary
		kept := []map[string]any{}
		for _, o := range p.Outbounds {
			tag, _ := o["tag"].(string)
			if merged[tag] {
				continue
			}
			for _, field := range []string{"detour", "default"} {
				if value, ok := o[field].(string); ok {
					o[field] = resolve(value)
				}
			}
			if o["outbounds"] != nil {
				refs := []string{}
				for _, ref := range stringsFrom(o["outbounds"]) {
					ref = resolve(ref)
					if !slices.Contains(refs, ref) {
						refs = append(refs, ref)
					}
				}
				o["outbounds"] = refs
			}
			kept = append(kept, o)
		}
		p.Outbounds = kept
		for _, r := range p.Rules {
			rewriteProxyRefs(r, resolve)
		}
		for _, r := range p.RuleSets {
			rewriteProxyRefs(r, resolve)
		}
		rewriteProxyRefs(p.DNS, resolve)
	}
	prepared.proxyRedirects = map[string]string{}
	for tag := range aliases {
		if target := resolve(tag); target != tag {
			prepared.proxyRedirects[tag] = target
		}
	}
	return &prepared, nil
}

// 只替换出站引用，不替换域名、规则集标签或其他恰好同名的字符串。
func rewriteProxyRefs(value any, resolve func(string) string) {
	switch object := value.(type) {
	case map[string]any:
		for key, child := range object {
			if key == "outbound" || key == "detour" || key == "download_detour" {
				switch ref := child.(type) {
				case string:
					object[key] = resolve(ref)
				case []any:
					for i, item := range ref {
						if tag, ok := item.(string); ok {
							ref[i] = resolve(tag)
						}
					}
				}
			} else {
				rewriteProxyRefs(child, resolve)
			}
		}
	case []any:
		for _, child := range object {
			rewriteProxyRefs(child, resolve)
		}
	}
}

// 保留规则、DNS、规则集下载和出站 detour 可达的节点；显式启用的过滤器仍可操作。
func (b *ConfigBuilder) pruneProxyOutbounds(c *SingBoxConfig) {
	byTag := map[string]Outbound{}
	for _, o := range c.Outbounds {
		tag, _ := o["tag"].(string)
		byTag[tag] = o
	}
	keep := map[string]bool{}
	var visit func(string)
	visit = func(tag string) {
		if keep[tag] || byTag[tag] == nil {
			return
		}
		keep[tag] = true
		o := byTag[tag]
		for _, ref := range stringsFrom(o["outbounds"]) {
			visit(ref)
		}
		if ref, ok := o["detour"].(string); ok {
			visit(ref)
		}
	}
	visit(c.Route.Final)
	if b.settings.ProxyPlan.ManagedOnly {
		visit("GLOBAL")
	}
	visit("DIRECT")
	visit("REJECT")
	for _, f := range b.filters {
		if f.Enabled {
			visit(f.Name)
		}
	}
	// 统一为普通 JSON 容器，包含 Raw DNS 和扩展规则集里的引用。
	raw, _ := json.Marshal(map[string]any{"route": c.Route, "dns": c.DNS})
	var document any
	_ = json.Unmarshal(raw, &document)
	rewriteProxyRefs(document, func(tag string) string { visit(tag); return tag })
	kept := []Outbound{}
	for _, o := range c.Outbounds {
		tag, _ := o["tag"].(string)
		if keep[tag] {
			kept = append(kept, o)
		}
	}
	c.Outbounds = kept
}

// ProxyRedirects 供规则总览显示整理后的出口，原始可编辑规则仍保持不变。
func (b *ConfigBuilder) ProxyRedirects() map[string]string {
	if b.settings.ProxyPlan == nil {
		return nil
	}
	prepared, err := b.prepareProxyPlan()
	if err != nil {
		return nil
	}
	return prepared.proxyRedirects
}
