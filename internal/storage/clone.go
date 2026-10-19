package storage

// 订阅节点占数据体积的绝大部分；JSON 往返克隆在路由器上每次需要数十毫秒和数 MB 临时内存。
// 写入入口先用 clone 规范为 JSON 类型，之后的读取只需按 JSON 值结构复制。

func cloneJSONValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = cloneJSONValue(e)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = cloneJSONValue(e)
		}
		return s
	default:
		return v
	}
}

// cloneNode 与 JSON 往返保持一致：来源信息不落盘，空 Extra 视为未设置。
func cloneNode(n Node) Node {
	n.SubscriptionID = ""
	if len(n.Extra) == 0 {
		n.Extra = nil
	} else {
		n.Extra = cloneJSONValue(n.Extra).(map[string]any)
	}
	return n
}

func cloneSubscription(sub Subscription) Subscription {
	if sub.ExpireAt != nil {
		v := *sub.ExpireAt
		sub.ExpireAt = &v
	}
	if sub.Traffic != nil {
		v := *sub.Traffic
		sub.Traffic = &v
	}
	if sub.Nodes != nil {
		nodes := make([]Node, len(sub.Nodes))
		for i := range sub.Nodes {
			nodes[i] = cloneNode(sub.Nodes[i])
		}
		sub.Nodes = nodes
	}
	return sub
}

func cloneSubscriptions(subs []Subscription) []Subscription {
	if subs == nil {
		return nil
	}
	result := make([]Subscription, len(subs))
	for i := range subs {
		result[i] = cloneSubscription(subs[i])
	}
	return result
}

func cloneManualNodes(nodes []ManualNode) []ManualNode {
	if nodes == nil {
		return nil
	}
	result := make([]ManualNode, len(nodes))
	for i, n := range nodes {
		n.Node = cloneNode(n.Node)
		result[i] = n
	}
	return result
}

// cloneData 只对节点使用结构复制，其余体积小的部分沿用 JSON 克隆以免遗漏新增字段。
func cloneData(d *AppData) *AppData {
	if d == nil {
		return nil
	}
	return &AppData{
		SchemaVersion: d.SchemaVersion,
		Subscriptions: cloneSubscriptions(d.Subscriptions),
		ManualNodes:   cloneManualNodes(d.ManualNodes),
		Filters:       clone(d.Filters),
		Rules:         clone(d.Rules),
		RuleGroups:    clone(d.RuleGroups),
		Settings:      clone(d.Settings),
	}
}
