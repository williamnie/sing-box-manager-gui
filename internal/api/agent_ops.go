package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// agentOp 是变更集中的一项类型化操作；不提供路径、端口、监听或内核等字段。
type agentOp struct {
	Action   string          `json:"action"`
	Resource string          `json:"resource"`
	ID       string          `json:"id,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	// Resolved 保存创建时拉取/解析的结果，应用时不再访问网络，保证与预览一致。
	Resolved json.RawMessage `json:"resolved,omitempty"`
}

type opResource struct {
	Name        string   `json:"resource"`
	Actions     []string `json:"actions"`
	Description string   `json:"description"`
	Fields      []string `json:"fields,omitempty"`
	Notes       string   `json:"notes,omitempty"`
}

// agentResources 是 CLI 与 Skill 的契约，capabilities/schema 原样返回。
var agentResources = []opResource{
	{"rule", []string{"create", "update", "delete"}, "自定义分流规则", []string{"name", "rule_type", "values", "outbound", "enabled", "priority", "source_cidrs", "network", "protocol", "ports", "port_ranges"}, "rule_type: domain/domain_suffix/domain_keyword/ip_cidr/geosite/geoip/port/port_range/match；网关拒绝 process_name；自定义拦截集合请用 blocklist"},
	{"rule_group", []string{"update"}, "预设规则组开关与出站", []string{"enabled", "outbound"}, "不能新增或删除预设组"},
	{"filter", []string{"create", "update", "delete"}, "节点过滤器（代理分组）", []string{"name", "include", "exclude", "include_countries", "exclude_countries", "mode", "urltest_config", "subscriptions", "all_nodes", "node_tags", "enabled"}, "node_tags 为 null 表示按条件动态匹配，[] 表示不选节点"},
	{"subscription", []string{"create", "update", "delete", "refresh"}, "订阅", []string{"name", "url", "enabled"}, "create/refresh 在创建变更集时拉取并固定结果；update 只允许 name/enabled；url 只能通过 --value-file/stdin 传入，读取时脱敏"},
	{"manual_node", []string{"create", "update", "delete"}, "手动节点", []string{"url", "node", "enabled"}, "create 可传分享链接 url 或完整 node；密钥类字段读取时脱敏"},
	{"host", []string{"create", "update", "delete"}, "DNS hosts 映射", []string{"domain", "ips", "enabled"}, ""},
	{"split_dns", []string{"create", "update", "delete"}, "Split DNS", []string{"domain_suffix", "source_cidrs", "server"}, "server: direct/proxy"},
	{"device_group", []string{"create", "update", "delete"}, "设备策略组", []string{"name", "policy", "outbound"}, "policy: split/direct/strict/bypass；导致网关规范化配置（含 bypass 网段）变化的变更会被拒绝"},
	{"device", []string{"create", "update", "delete"}, "设备", []string{"name", "addresses", "group_id", "enabled"}, "启停或改地址若影响 bypass 网段会被拒绝"},
	{"dns", []string{"update"}, "DNS 与默认出站", []string{"proxy_dns", "direct_dns", "final_outbound", "log_level"}, "其他设置（端口、路径、监听、内核、自动应用）不开放"},
	{"blocklist", []string{"add", "remove"}, "自定义域名拦截集合", []string{"domains"}, "value: {\"domains\": [\"ads.example.com\"]}"},
	{"imported_rule", []string{"update", "delete"}, "导入规则（来自配置导入，保持原有顺序）", []string{"revision", "updates"}, "id 为 get imported_rule 返回的索引；value.revision 必须等于返回的 revision；updates 只允许 domain/domain_suffix/domain_keyword/domain_regex/ip_cidr/source_ip_cidr/rule_set/network/protocol/port/source_port/port_range/source_port_range/invert/ip_is_private/source_ip_is_private/action(route|reject)/outbound，值为 null 表示删除该条件；未列出的扩展字段原样保留；必须单独成一个变更集"},
	{"revert", []string{"apply"}, "撤销已确认的变更", nil, "id 为已确认的变更集；仅在其后没有任何其他修改时可用，否则需新建变更；必须单独成一个变更集，仍需预览、应用并确认；只保留最近 20 个已确认变更的恢复数据"},
}

var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

type opEnv struct {
	gateway   bool
	fetchSub  func(*storage.Subscription) error
	parseNode func(string) (*storage.Node, error)
	now       time.Time
}

func strictDecode(raw []byte, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("字段无效：%s", strings.TrimPrefix(err.Error(), "json: "))
	}
	if d.More() {
		return fmt.Errorf("只能提交一个 JSON 对象")
	}
	return nil
}

func patchKeys(raw json.RawMessage) (map[string]json.RawMessage, error) {
	patch := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &patch); err != nil || len(patch) == 0 {
		return nil, fmt.Errorf("update 需要包含字段的 JSON 对象")
	}
	return patch, nil
}

// mergeStrict 把补丁字段覆盖到现有对象上，再按目标类型严格解码。
func mergeStrict[T any](current T, raw json.RawMessage, allowed []string) (T, error) {
	var result T
	patch, err := patchKeys(raw)
	if err != nil {
		return result, err
	}
	for key := range patch {
		if key == "id" {
			return result, fmt.Errorf("不能修改 id")
		}
		if allowed != nil && !contains(allowed, key) {
			return result, fmt.Errorf("字段 %s 不允许修改", key)
		}
	}
	base, _ := json.Marshal(current)
	merged := map[string]json.RawMessage{}
	_ = json.Unmarshal(base, &merged)
	for k, v := range patch {
		merged[k] = v
	}
	out, _ := json.Marshal(merged)
	err = strictDecode(out, &result)
	return result, err
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// crud 统一处理带 ID 的列表资源。
func crud[T any](items *[]T, op *agentOp, getID func(*T) string, setID func(*T, string), allowed []string, validate func(T) error) error {
	index := -1
	for i := range *items {
		if getID(&(*items)[i]) == op.ID {
			index = i
		}
	}
	switch op.Action {
	case "create":
		if index >= 0 {
			return fmt.Errorf("%s %s 已存在", op.Resource, op.ID)
		}
		var item T
		if err := strictDecode(op.Value, &item); err != nil {
			return err
		}
		setID(&item, op.ID)
		if validate != nil {
			if err := validate(item); err != nil {
				return err
			}
		}
		*items = append(*items, item)
	case "update":
		if index < 0 {
			return fmt.Errorf("%s %s 不存在", op.Resource, op.ID)
		}
		item, err := mergeStrict((*items)[index], op.Value, allowed)
		if err != nil {
			return err
		}
		setID(&item, op.ID)
		if validate != nil {
			if err := validate(item); err != nil {
				return err
			}
		}
		(*items)[index] = item
	case "delete":
		if index < 0 {
			return fmt.Errorf("%s %s 不存在", op.Resource, op.ID)
		}
		*items = append((*items)[:index], (*items)[index+1:]...)
	default:
		return fmt.Errorf("%s 不支持 %s", op.Resource, op.Action)
	}
	return nil
}

func resourceSpec(name string) *opResource {
	for i := range agentResources {
		if agentResources[i].Name == name {
			return &agentResources[i]
		}
	}
	return nil
}

// prepareOp 校验操作形状、分配 ID，并在需要时访问网络固定结果；只在创建变更集时执行。
func prepareOp(op *agentOp, env opEnv) error {
	spec := resourceSpec(op.Resource)
	if spec == nil {
		return fmt.Errorf("未知资源 %q", op.Resource)
	}
	if !contains(spec.Actions, op.Action) {
		return fmt.Errorf("%s 不支持 %s（允许：%s）", op.Resource, op.Action, strings.Join(spec.Actions, "/"))
	}
	op.Resolved = nil
	if op.Resource == "dns" || op.Resource == "blocklist" {
		op.ID = ""
	} else if op.Action == "create" && op.ID == "" {
		op.ID = uuid.New().String()
	}
	if op.ID != "" && !agentIDPattern.MatchString(op.ID) {
		return fmt.Errorf("id 只能包含字母、数字和 _.:-，最长 64")
	}
	if op.ID == "" && op.Resource != "dns" && op.Resource != "blocklist" {
		return fmt.Errorf("%s %s 需要 id", op.Resource, op.Action)
	}
	if op.Resource == "revert" && len(bytes.TrimSpace(op.Value)) > 0 {
		return fmt.Errorf("revert 不接受 value")
	}
	if op.Resource == "imported_rule" {
		if n, err := strconv.Atoi(op.ID); err != nil || n < 0 || strconv.Itoa(n) != op.ID {
			return fmt.Errorf("imported_rule 的 id 是规则索引（非负整数）")
		}
		if len(bytes.TrimSpace(op.Value)) == 0 {
			return fmt.Errorf("imported_rule 需要 value.revision")
		}
	}
	if op.Action != "delete" && op.Action != "refresh" && op.Action != "apply" && len(bytes.TrimSpace(op.Value)) == 0 {
		return fmt.Errorf("%s %s 需要 value", op.Resource, op.Action)
	}
	switch {
	case op.Resource == "subscription" && op.Action == "create":
		var req struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := strictDecode(op.Value, &req); err != nil {
			return err
		}
		if strings.TrimSpace(req.Name) == "" || !(strings.HasPrefix(req.URL, "https://") || strings.HasPrefix(req.URL, "http://")) {
			return fmt.Errorf("订阅需要 name 和 http(s) url")
		}
		sub := storage.Subscription{ID: op.ID, Name: strings.TrimSpace(req.Name), URL: req.URL, Nodes: []storage.Node{}, Enabled: true, UpdatedAt: env.now}
		if err := env.fetchSub(&sub); err != nil {
			return err
		}
		if err := validateAgentNodes(sub.Nodes); err != nil {
			return err
		}
		op.Resolved, _ = json.Marshal(sub)
	case op.Resource == "manual_node" && op.Action == "create":
		var req struct {
			URL     string        `json:"url"`
			Node    *storage.Node `json:"node"`
			Enabled *bool         `json:"enabled"`
		}
		if err := strictDecode(op.Value, &req); err != nil {
			return err
		}
		node := req.Node
		if (req.URL == "") == (node == nil) {
			return fmt.Errorf("manual_node create 需要 url 或 node 之一")
		}
		if req.URL != "" {
			parsed, err := env.parseNode(req.URL)
			if err != nil {
				return fmt.Errorf("节点链接解析失败")
			}
			node = parsed
		}
		if node.Tag == "" || node.Type == "" || node.Server == "" || node.ServerPort < 1 || node.ServerPort > 65535 {
			return fmt.Errorf("节点需要 tag、type、server 和有效端口")
		}
		if err := validateAgentNode(*node); err != nil {
			return err
		}
		enabled := req.Enabled == nil || *req.Enabled
		op.Resolved, _ = json.Marshal(storage.ManualNode{ID: op.ID, Node: *node, Enabled: enabled})
	}
	return nil
}

// refreshSubscriptionOp 在 prepare 阶段需要当前订阅，因此由调用方单独执行。
func prepareRefresh(op *agentOp, data *storage.AppData, env opEnv) error {
	for _, sub := range data.Subscriptions {
		if sub.ID == op.ID {
			copy := sub
			if err := env.fetchSub(&copy); err != nil {
				return err
			}
			if err := validateAgentNodes(copy.Nodes); err != nil {
				return err
			}
			copy.UpdatedAt = env.now
			op.Resolved, _ = json.Marshal(copy)
			return nil
		}
	}
	return fmt.Errorf("subscription %s 不存在", op.ID)
}

// applyOp 只修改内存中的候选数据，不访问网络或磁盘。
func applyOp(data *storage.AppData, op *agentOp, env opEnv) error {
	s := data.Settings
	switch op.Resource {
	case "rule":
		if op.ID == domainBlocklistID {
			return fmt.Errorf("自定义拦截集合请使用 blocklist 资源")
		}
		return crud(&data.Rules, op, func(r *storage.Rule) string { return r.ID }, func(r *storage.Rule, id string) { r.ID = id }, nil,
			func(r storage.Rule) error { return storage.ValidateRule(r, env.gateway) })
	case "rule_group":
		return crud(&data.RuleGroups, op, func(r *storage.RuleGroup) string { return r.ID }, func(r *storage.RuleGroup, id string) { r.ID = id }, []string{"enabled", "outbound"}, nil)
	case "filter":
		return crud(&data.Filters, op, func(f *storage.Filter) string { return f.ID }, func(f *storage.Filter, id string) { f.ID = id }, nil,
			func(f storage.Filter) error {
				if strings.TrimSpace(f.Name) == "" || (f.Mode != "urltest" && f.Mode != "select") {
					return fmt.Errorf("过滤器需要 name，mode 为 urltest 或 select")
				}
				return nil
			})
	case "subscription":
		switch op.Action {
		case "create", "refresh":
			var sub storage.Subscription
			if err := json.Unmarshal(op.Resolved, &sub); err != nil || sub.ID != op.ID {
				return fmt.Errorf("订阅拉取结果缺失，请重新创建变更集")
			}
			for i := range data.Subscriptions {
				if data.Subscriptions[i].ID == op.ID {
					if op.Action == "create" {
						return fmt.Errorf("subscription %s 已存在", op.ID)
					}
					// 刷新只替换节点与流量，保留名称、URL 和开关。
					cur := &data.Subscriptions[i]
					cur.Nodes, cur.NodeCount, cur.UpdatedAt, cur.Traffic, cur.ExpireAt = sub.Nodes, len(sub.Nodes), sub.UpdatedAt, sub.Traffic, sub.ExpireAt
					return nil
				}
			}
			if op.Action == "refresh" {
				return fmt.Errorf("subscription %s 不存在", op.ID)
			}
			data.Subscriptions = append(data.Subscriptions, sub)
			return nil
		}
		return crud(&data.Subscriptions, op, func(s *storage.Subscription) string { return s.ID }, func(s *storage.Subscription, id string) { s.ID = id }, []string{"name", "enabled"}, nil)
	case "manual_node":
		if op.Action == "create" {
			var node storage.ManualNode
			if err := json.Unmarshal(op.Resolved, &node); err != nil || node.ID != op.ID {
				return fmt.Errorf("节点解析结果缺失，请重新创建变更集")
			}
			for _, n := range data.ManualNodes {
				if n.ID == op.ID {
					return fmt.Errorf("manual_node %s 已存在", op.ID)
				}
			}
			data.ManualNodes = append(data.ManualNodes, node)
			return nil
		}
		return crud(&data.ManualNodes, op, func(n *storage.ManualNode) string { return n.ID }, func(n *storage.ManualNode, id string) { n.ID = id }, []string{"node", "enabled"},
			func(n storage.ManualNode) error { return validateAgentNode(n.Node) })
	case "host":
		return crud(&s.Hosts, op, func(h *storage.HostEntry) string { return h.ID }, func(h *storage.HostEntry, id string) { h.ID = id }, nil, nil)
	case "split_dns":
		return crud(&s.SplitDNS, op, func(r *storage.SplitDNSRule) string { return r.ID }, func(r *storage.SplitDNSRule, id string) { r.ID = id }, nil, nil)
	case "device_group":
		return crud(&s.DeviceGroups, op, func(g *storage.DeviceGroup) string { return g.ID }, func(g *storage.DeviceGroup, id string) { g.ID = id }, nil, nil)
	case "device":
		return crud(&s.Devices, op, func(d *storage.Device) string { return d.ID }, func(d *storage.Device, id string) { d.ID = id }, nil, nil)
	case "dns":
		var req struct {
			ProxyDNS      *string `json:"proxy_dns"`
			DirectDNS     *string `json:"direct_dns"`
			FinalOutbound *string `json:"final_outbound"`
			LogLevel      *string `json:"log_level"`
		}
		if _, err := patchKeys(op.Value); err != nil {
			return err
		}
		if err := strictDecode(op.Value, &req); err != nil {
			return err
		}
		for _, p := range []struct {
			src *string
			dst *string
		}{{req.ProxyDNS, &s.ProxyDNS}, {req.DirectDNS, &s.DirectDNS}, {req.FinalOutbound, &s.FinalOutbound}, {req.LogLevel, &s.LogLevel}} {
			if p.src != nil {
				if strings.TrimSpace(*p.src) == "" {
					return fmt.Errorf("DNS 与出站字段不能为空")
				}
				*p.dst = strings.TrimSpace(*p.src)
			}
		}
		return nil
	case "blocklist":
		return applyBlocklistOp(data, op)
	case "imported_rule":
		return applyImportedRuleOp(data, op)
	case "revert":
		var p revertPayload
		if err := json.Unmarshal(op.Resolved, &p); err != nil || p.Store == nil || p.Store.Settings == nil {
			return fmt.Errorf("撤销数据缺失，请重新创建变更集")
		}
		// 只在目标变更确认后没有任何其他修改时整体恢复，避免覆盖后续数据。
		if storeDigest(data) != p.ExpectRev {
			return agentFail(409, "revert_conflict", "目标变更确认后已有其他修改，不能自动撤销；请改为创建新的变更")
		}
		*data = *deepCopy(p.Store)
		return nil
	}
	return fmt.Errorf("未知资源 %q", op.Resource)
}

func applyBlocklistOp(data *storage.AppData, op *agentOp) error {
	var req struct {
		Domains []string `json:"domains"`
	}
	if err := strictDecode(op.Value, &req); err != nil {
		return err
	}
	changes, err := normalizeBlockedDomains(req.Domains)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return fmt.Errorf("blocklist 需要至少一个域名")
	}
	rule, _ := domainBlocklist(data.Rules)
	if !plainDomainBlocklist(rule) {
		return fmt.Errorf("自定义拦截集合的匹配方式已改变，请在界面检查该规则")
	}
	if rule != nil && !rule.Enabled {
		return fmt.Errorf("自定义拦截集合已停用，请先在界面启用")
	}
	current := []string{}
	if rule != nil {
		current = rule.Values
	}
	next := []string{}
	if op.Action == "add" {
		next = append(append(next, current...), changes...)
	} else {
		for _, d := range current {
			if !contains(changes, strings.TrimSuffix(strings.ToLower(d), ".")) {
				next = append(next, d)
			}
		}
	}
	domains, err := normalizeBlockedDomains(next)
	if err != nil {
		return err
	}
	for i := range data.Rules {
		if data.Rules[i].ID == domainBlocklistID {
			if len(domains) == 0 {
				data.Rules = append(data.Rules[:i], data.Rules[i+1:]...)
			} else {
				data.Rules[i].Values = domains
			}
			return nil
		}
	}
	if len(domains) == 0 {
		return nil
	}
	priority := 0
	for _, r := range data.Rules {
		if r.Priority <= priority {
			if r.Priority == math.MinInt {
				return fmt.Errorf("现有规则优先级已达下限，请先调整优先级")
			}
			priority = r.Priority - 1
		}
	}
	data.Rules = append(data.Rules, storage.Rule{ID: domainBlocklistID, Name: "自定义域名拦截", RuleType: "domain", Values: domains, Outbound: "REJECT", Enabled: true, Priority: priority})
	return nil
}

// deepCopy 通过 JSON 往返得到与存储层一致的独立副本。
func deepCopy[T any](value T) T {
	b, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(b, &result)
	return result
}

func applyImportedRuleOp(data *storage.AppData, op *agentOp) error {
	var req struct {
		Revision string         `json:"revision"`
		Updates  map[string]any `json:"updates"`
	}
	if err := strictDecode(op.Value, &req); err != nil {
		return err
	}
	policy := data.Settings.ImportedPolicy
	index, _ := strconv.Atoi(op.ID)
	if policy == nil || index >= len(policy.Rules) {
		return fmt.Errorf("导入规则 %s 不存在，请重新读取 get imported_rule", op.ID)
	}
	if req.Revision == "" || req.Revision != importedPolicyRevision(policy) {
		return agentFail(409, "imported_revision_changed", "导入策略版本已变化，请重新读取 get imported_rule 后再修改")
	}
	switch op.Action {
	case "update":
		if len(req.Updates) == 0 {
			return fmt.Errorf("update 需要 updates")
		}
		rule, err := mergeImportedRule(policy.Rules[index], req.Updates)
		if err != nil {
			return err
		}
		policy.Rules[index] = rule
	case "delete":
		if len(req.Updates) > 0 {
			return fmt.Errorf("delete 不接受 updates")
		}
		policy.Rules = append(policy.Rules[:index], policy.Rules[index+1:]...)
	}
	return nil
}
