package migration

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// AdoptManagedNodes 将旧策略中的分组引用并入默认代理。
// 只将规则、DNS 或 detour 直接需要的独有节点迁入可编辑的手动节点，旧分组成员不再作为节点来源。
func AdoptManagedNodes(data *storage.AppData) ([]string, error) {
	policy := data.Settings.ImportedPolicy
	if policy == nil {
		policy = &storage.ImportedPolicy{}
	}
	originals := map[string]map[string]any{}
	aliases := map[string]string{}
	kept := []map[string]any{}
	for _, o := range policy.Outbounds {
		tag, _ := o["tag"].(string)
		originals[tag] = o
		switch o["type"] {
		case "selector", "urltest":
			aliases[tag] = "Proxy"
		case "direct", "block":
			if len(o) == 2 {
				if o["type"] == "direct" {
					aliases[tag] = "DIRECT"
				} else {
					aliases[tag] = "REJECT"
				}
			} else {
				kept = append(kept, o)
			}
		}
	}
	for _, f := range data.Filters {
		if !f.Enabled {
			aliases[f.Name] = "Proxy"
		}
	}
	existing := map[string]storage.Node{}
	for _, s := range data.Subscriptions {
		if s.Enabled {
			for _, n := range s.Nodes {
				existing[n.Tag] = n
			}
		}
	}
	for _, m := range data.ManualNodes {
		if m.Enabled {
			existing[m.Node.Tag] = m.Node
		}
	}
	adopted := []string{}
	visiting := map[string]bool{}
	var resolve func(string) (string, error)
	var rewrite func(any) error
	rewrite = func(value any) error {
		switch object := value.(type) {
		case map[string]any:
			for key, child := range object {
				if key == "outbound" || key == "detour" || key == "download_detour" {
					switch ref := child.(type) {
					case string:
						next, err := resolve(ref)
						if err != nil {
							return err
						}
						object[key] = next
					case []any:
						for i, x := range ref {
							if tag, ok := x.(string); ok {
								next, err := resolve(tag)
								if err != nil {
									return err
								}
								ref[i] = next
							}
						}
					}
				} else if err := rewrite(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range object {
				if err := rewrite(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	resolve = func(tag string) (string, error) {
		if target, ok := aliases[tag]; ok {
			return target, nil
		}
		original := originals[tag]
		if original == nil || original["type"] == "direct" || original["type"] == "block" {
			return tag, nil
		}
		if visiting[tag] {
			return "", fmt.Errorf("旧节点 detour 循环：%s", tag)
		}
		visiting[tag] = true
		defer delete(visiting, tag)
		raw, err := json.Marshal(original)
		if err != nil {
			return "", err
		}
		var outbound map[string]any
		if err := json.Unmarshal(raw, &outbound); err != nil {
			return "", err
		}
		if err := rewrite(outbound); err != nil {
			return "", err
		}
		names := make([]string, 0, len(existing))
		for name := range existing {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			node := existing[name]
			if equalManagedNode(node, outbound) {
				aliases[tag] = name
				return name, nil
			}
		}
		server, _ := outbound["server"].(string)
		port, _ := outbound["server_port"].(float64)
		kind, _ := outbound["type"].(string)
		if server == "" || port < 1 || port > 65535 {
			return "", fmt.Errorf("旧出站 %s 无法转换为可管理节点，请先修改引用", tag)
		}
		name := strings.TrimPrefix(tag, "SMbox/")
		if name == "" || name == "Proxy" || name == "DIRECT" || name == "REJECT" {
			name = "专用出口"
		}
		base := name
		for i := 2; existing[name].Tag != ""; i++ {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		for _, field := range []string{"tag", "type", "server", "server_port"} {
			delete(outbound, field)
		}
		node := storage.Node{Tag: name, Type: kind, Server: server, ServerPort: int(port), Extra: outbound}
		existing[name] = node
		aliases[tag] = name
		data.ManualNodes = append(data.ManualNodes, storage.ManualNode{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("managed-legacy:"+tag+":"+name)).String(), Enabled: true, Node: node})
		adopted = append(adopted, name)
		return name, nil
	}
	for i := range data.Rules {
		next, err := resolve(data.Rules[i].Outbound)
		if err != nil {
			return nil, err
		}
		data.Rules[i].Outbound = next
	}
	for i := range data.RuleGroups {
		next, err := resolve(data.RuleGroups[i].Outbound)
		if err != nil {
			return nil, err
		}
		data.RuleGroups[i].Outbound = next
	}
	for i := range data.Settings.DeviceGroups {
		next, err := resolve(data.Settings.DeviceGroups[i].Outbound)
		if err != nil {
			return nil, err
		}
		data.Settings.DeviceGroups[i].Outbound = next
	}
	for _, rule := range policy.Rules {
		if err := rewrite(rule); err != nil {
			return nil, err
		}
	}
	for _, ruleSet := range policy.RuleSets {
		if err := rewrite(ruleSet); err != nil {
			return nil, err
		}
	}
	if err := rewrite(policy.DNS); err != nil {
		return nil, err
	}
	for _, outbound := range kept {
		if err := rewrite(outbound); err != nil {
			return nil, err
		}
	}
	policy.Outbounds = kept
	policy.Final = "Proxy"
	data.Settings.FinalOutbound = "Proxy"
	return adopted, nil
}

func equalManagedNode(node storage.Node, outbound map[string]any) bool {
	left := map[string]any{"type": node.Type, "server": node.Server, "server_port": node.ServerPort}
	for key, value := range node.Extra {
		left[key] = value
	}
	right := map[string]any{}
	for key, value := range outbound {
		if key != "tag" {
			right[key] = value
		}
	}
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}
