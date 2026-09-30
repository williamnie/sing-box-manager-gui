package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

type interfaceAddresses struct {
	AddrInfo []struct {
		Local             string          `json:"local"`
		PrefixLen         int             `json:"prefixlen"`
		Flags             []string        `json:"flags"`
		Scope             string          `json:"scope"`
		Tentative         bool            `json:"tentative"`
		DADFailed         bool            `json:"dadfailed"`
		Deprecated        bool            `json:"deprecated"`
		PreferredLifeTime json.RawMessage `json:"preferred_life_time"`
	} `json:"addr_info"`
}

func hasConfiguredIPv6Prefixes(c Config, interfaces []interfaceAddresses) bool {
	actual := []netip.Prefix{}
	for _, iface := range interfaces {
		for _, a := range iface.AddrInfo {
			address, e := netip.ParseAddr(a.Local)
			if e != nil || !address.Is6() || address.IsLinkLocalUnicast() || !address.IsGlobalUnicast() || a.PrefixLen < 1 || a.PrefixLen > 128 || a.Tentative || a.DADFailed || a.Deprecated || string(a.PreferredLifeTime) == "0" || includes(a.Flags, "tentative") || includes(a.Flags, "dadfailed") || includes(a.Flags, "deprecated") {
				continue
			}
			actual = append(actual, netip.PrefixFrom(address, a.PrefixLen).Masked())
		}
	}
	for _, cidr := range c.LANCIDRs {
		configured, e := netip.ParsePrefix(cidr)
		if e != nil {
			return false
		}
		if !configured.Addr().Is6() {
			continue
		}
		matched := false
		for _, prefix := range actual {
			if prefix.Bits() <= configured.Bits() && prefix.Contains(configured.Addr()) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

type defaultRoute struct {
	Destination string          `json:"dst"`
	Gateway     string          `json:"gateway"`
	Device      string          `json:"dev"`
	Metric      uint64          `json:"metric"`
	Type        string          `json:"type"`
	Flags       []string        `json:"flags"`
	Nexthops    json.RawMessage `json:"nexthops"`
	NHID        int             `json:"nhid"`
}

// checkDefaultRoute 仅核验本机活动主路由，不把“能到达网关地址”误认为默认路由正确。
func (m *Manager) checkDefaultRoute(ctx context.Context, c Config, add func(string, string, string)) error {
	raw, e := m.run(ctx, "ip", "-j", "-4", "route", "show", "table", "main", "default")
	if e != nil {
		return fmt.Errorf("无法读取 IPv4 主路由表的默认路由: %w", e)
	}
	var routes []defaultRoute
	if e = json.Unmarshal([]byte(raw), &routes); e != nil {
		return fmt.Errorf("默认路由检查输出无效")
	}
	best := []defaultRoute{}
	var metric uint64
	for _, r := range routes {
		if r.Destination != "default" && r.Destination != "0.0.0.0/0" {
			continue
		}
		if includes(r.Flags, "linkdown") || includes(r.Flags, "dead") {
			continue
		}
		if len(best) == 0 || r.Metric < metric {
			best = []defaultRoute{r}
			metric = r.Metric
		} else if r.Metric == metric {
			best = append(best, r)
		}
	}
	if len(best) == 0 {
		add("error", "default_route_missing", "没有可用的 IPv4 主表默认路由；请先由系统管理员配置上游路由")
		return nil
	}
	for _, r := range best {
		if r.NHID != 0 || len(r.Nexthops) > 0 && string(r.Nexthops) != "null" && string(r.Nexthops) != "[]" || r.Type != "" && r.Type != "unicast" {
			add("error", "default_route_complex", "活动默认路由使用多路径、独立 nexthop 或非单播策略，需先核对并隔离复杂路由")
			return nil
		}
		if r.Device != c.UplinkInterface || r.Gateway != c.UpstreamGateway {
			add("error", "default_route_mismatch", "配置的上游接口/网关与最低 metric 的活动主表默认路由不一致，管理器不会自动修改路由")
			return nil
		}
	}
	return nil
}

func supportedPolicyRule(line string, applied bool) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return true
	}
	priority, e := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
	if e != nil {
		return false
	}
	if len(fields) == 5 && fields[1] == "from" && fields[2] == "all" && fields[3] == "lookup" {
		if priority == 0 && (fields[4] == "local" || fields[4] == "255") {
			return true
		}
		if priority == 32766 && (fields[4] == "main" || fields[4] == "254") {
			return true
		}
		if priority == 32767 && (fields[4] == "default" || fields[4] == "253") {
			return true
		}
	}
	// 只在已有本实例归属时容许 sing-box 的固定自动规则区间和 fallback。
	return applied && (priority >= 9000 && priority < 9020 || priority == 32768 && hasPair(line, "lookup", "main"))
}
