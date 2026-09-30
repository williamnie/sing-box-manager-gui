package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// DNS 的所有路由均属于单个保留表，规则同时限定入口、目的网段和完整 mark。
// 不创建 default 路由、不处理 OUTPUT，也不修改全局 forwarding。
type dnsRouting struct {
	Routes string `json:"routes"`
	Rules  string `json:"rules"`
}

func previewDNS(c Config) Plan {
	p := Plan{Config: c, ConfigDigest: configDigest(c), Sysctls: map[string]string{
		// max(all, interface) 采用 loose 模式即可接受保留网段的透明入站，其他接口保持原值。
		"net/ipv4/conf/" + c.LANInterface + "/rp_filter": "2",
	}, Warnings: []string{
		"主路由 DHCP 必须直接下发本机 LAN DNS；客户端默认网关保持主路由，不接管 DHCP",
		"主路由必须配置 FakeIP 网段 → 旁路 LAN 地址静态路由；已有路由可复用，界面确认不等于现场验证",
		"只接收指定 LAN 接口、LAN 来源和 FakeIP 目的的 TCP/UDP；真实 IP、BT/PT 直连、硬编码 IP、自带 DoH 和 IPv6 备用路径仍可经主路由直出",
		"IPv4-only FakeIP：代理域名 AAAA 不返回真实 IPv6；此模式不能保证严格全代理或 WebRTC 零泄露",
		"不会开启整机转发、NAT 或接管 Docker；现有防火墙须允许 LAN DNS 及透明入站，不能仅凭配置检查视为流量验收",
	}}
	if c.DNSSource == "router" {
		p.Warnings = append(p.Warnings, "主路由转发 DNS 会合并客户端来源，DNS 阶段不能识别原始终端或应用设备差异策略")
	}
	var v4 []string
	for _, cidr := range c.LANCIDRs {
		if netip.MustParsePrefix(cidr).Addr().Is4() {
			v4 = append(v4, cidr)
		}
	}
	var n strings.Builder
	fmt.Fprintf(&n, "table inet %s {\n comment \"singbox-manager dns bypass owned v1\"\n", TableName)
	n.WriteString(" chain dns_access {\n  type filter hook input priority -10; policy accept;\n  iifname \"lo\" return\n")
	fmt.Fprintf(&n, "  ip daddr %s meta l4proto { tcp, udp } th dport %d iifname != \"%s\" counter drop\n", c.LANAddress, c.DNSPort, c.LANInterface)
	fmt.Fprintf(&n, "  ip daddr %s meta l4proto { tcp, udp } th dport %d ip saddr != { %s } counter drop\n", c.LANAddress, c.DNSPort, strings.Join(v4, ", "))
	// 普通客户端不能直接连接透明监听端口；原目的为 FakeIP 的包保留原目的端口。
	fmt.Fprintf(&n, "  ip daddr != %s meta l4proto { tcp, udp } th dport %d counter drop\n", c.FakeIPRange, TProxyPort)
	n.WriteString(" }\n chain fakeip_ingress {\n  type filter hook prerouting priority mangle; policy accept;\n")
	fmt.Fprintf(&n, "  iifname \"%s\" ip saddr { %s } ip daddr %s meta l4proto { tcp, udp } meta mark set %s tproxy ip to :%d counter accept\n", c.LANInterface, strings.Join(v4, ", "), c.FakeIPRange, DNSMark, TProxyPort)
	n.WriteString(" }\n}\n")
	p.NFTables = n.String()
	p.PolicyRoutes = []string{"ip " + strings.Join(dnsRouteArgs("add", c), " "), "ip " + strings.Join(dnsRuleArgs("add", c), " ")}
	return p
}

func dnsRouteArgs(action string, c Config) []string {
	return []string{"-4", "route", action, "local", c.FakeIPRange, "dev", "lo", "table", strconv.Itoa(DNSRouteTable), "proto", "242"}
}
func dnsRuleArgs(action string, c Config) []string {
	return []string{"-4", "rule", action, "priority", strconv.Itoa(DNSRulePriority), "from", "all", "to", c.FakeIPRange, "iif", c.LANInterface, "fwmark", DNSMark + "/0xffffffff", "lookup", strconv.Itoa(DNSRouteTable), "protocol", "242"}
}
func canonicalLines(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			lines = append(lines, strings.Join(f, " "))
		}
	}
	return strings.Join(lines, "\n")
}
func reservedDNSRule(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	if strings.TrimSuffix(f[0], ":") == strconv.Itoa(DNSRulePriority) || hasPair(line, "lookup", strconv.Itoa(DNSRouteTable)) {
		return true
	}
	for i, v := range f {
		if v == "fwmark" && i+1 < len(f) && strings.Split(f[i+1], "/")[0] == DNSMark {
			return true
		}
	}
	return false
}
func (m *Manager) captureDNSRouting(ctx context.Context) (dnsRouting, error) {
	var r dnsRouting
	raw, e := m.run(ctx, "ip", "-N", "-4", "route", "show", "table", strconv.Itoa(DNSRouteTable))
	if e != nil && !strings.Contains(raw, "FIB table does not exist") {
		return r, e
	}
	if e == nil {
		r.Routes = canonicalLines(raw)
	}
	raw, e = m.run(ctx, "ip", "-N", "-4", "rule", "show")
	if e != nil {
		return r, e
	}
	var rules []string
	for _, line := range strings.Split(raw, "\n") {
		if reservedDNSRule(line) {
			rules = append(rules, line)
		}
	}
	r.Rules = canonicalLines(strings.Join(rules, "\n"))
	return r, nil
}

func validDNSRoute(s string, c Config) bool {
	f := strings.Fields(s)
	if len(f) < 2 || f[0] != "2" || f[1] != c.FakeIPRange {
		return false
	}
	seen := map[string]string{}
	for i := 2; i < len(f); i += 2 {
		if i+1 >= len(f) || seen[f[i]] != "" {
			return false
		}
		seen[f[i]] = f[i+1]
	}
	return len(seen) == 3 && seen["dev"] == "lo" && seen["proto"] == "242" && seen["scope"] == "254"
}
func validDNSRule(s string, c Config) bool {
	f := strings.Fields(s)
	if len(f) < 1 || f[0] != strconv.Itoa(DNSRulePriority)+":" {
		return false
	}
	seen := map[string]string{}
	for i := 1; i < len(f); i += 2 {
		if i+1 >= len(f) || seen[f[i]] != "" {
			return false
		}
		seen[f[i]] = f[i+1]
	}
	mark := seen["fwmark"]
	return len(seen) == 6 && seen["from"] == "all" && seen["to"] == c.FakeIPRange && seen["iif"] == c.LANInterface && (mark == DNSMark || mark == DNSMark+"/0xffffffff") && seen["lookup"] == strconv.Itoa(DNSRouteTable) && seen["proto"] == "242"
}

func (m *Manager) applyDNSRouting(ctx context.Context, s *state) error {
	tx := s.Pending
	if tx.Target.Config.AccessMode != "dns" {
		return nil
	}
	// 修改 FakeIP 地址池/接口须先恢复，避免出现旧映射与新路由并存的半更新。
	if tx.Before.DNSRouting != (dnsRouting{}) {
		return nil
	}
	for _, step := range []struct {
		args  []string
		route bool
	}{{dnsRouteArgs("add", tx.Target.Config), true}, {dnsRuleArgs("add", tx.Target.Config), false}} {
		before, e := m.captureDNSRouting(ctx)
		if e != nil {
			return e
		}
		if before != tx.ObservedDNSRouting {
			return fmt.Errorf("DNS 策略路由在应用期间被外部修改")
		}
		if _, e = m.run(ctx, "ip", step.args...); e != nil {
			return e
		}
		current, e := m.captureDNSRouting(ctx)
		if e != nil {
			return e
		}
		if !validDNSRoute(current.Routes, tx.Target.Config) || step.route && current.Rules != "" || !step.route && !validDNSRule(current.Rules, tx.Target.Config) {
			return fmt.Errorf("DNS 路由应用结果不符合计划，拒绝记录未知资源")
		}
		tx.ObservedDNSRouting = current
		if e = m.save(*s); e != nil {
			return e
		}
	}
	return nil
}

// 部分恢复允许某一组件已经回到 baseline，但绝不删除未持久记录的路由/规则。
func (m *Manager) restoreDNSRouting(ctx context.Context, tx *transaction, live dnsRouting) error {
	if tx.Target.Config.AccessMode != "dns" {
		return nil
	}
	target, owned := tx.Before.DNSRouting, tx.ObservedDNSRouting
	if live.Routes != target.Routes && live.Routes != owned.Routes || live.Rules != target.Rules && live.Rules != owned.Rules {
		return fmt.Errorf("DNS 策略路由已漂移或崩溃窗口没有归属指纹，拒绝覆盖")
	}
	if target != (dnsRouting{}) {
		if live != target {
			return fmt.Errorf("DNS 路由更新不完整，需管理员核对")
		}
		return nil
	}
	if live.Rules != "" {
		if !validDNSRule(live.Rules, tx.Target.Config) {
			return fmt.Errorf("拒绝删除未知 DNS 策略规则")
		}
		now, e := m.captureDNSRouting(ctx)
		if e != nil || now != live {
			return fmt.Errorf("DNS 策略规则恢复前发生变化")
		}
		if _, e = m.run(ctx, "ip", dnsRuleArgs("del", tx.Target.Config)...); e != nil {
			return e
		}
		live.Rules = ""
	}
	if live.Routes != "" {
		if !validDNSRoute(live.Routes, tx.Target.Config) {
			return fmt.Errorf("拒绝删除未知 DNS 本地路由")
		}
		now, e := m.captureDNSRouting(ctx)
		if e != nil || now != live {
			return fmt.Errorf("DNS 路由恢复前发生变化")
		}
		if _, e = m.run(ctx, "ip", dnsRouteArgs("del", tx.Target.Config)...); e != nil {
			return e
		}
	}
	now, e := m.captureDNSRouting(ctx)
	if e != nil {
		return e
	}
	if now != target {
		return fmt.Errorf("DNS 路由恢复健康检查失败")
	}
	return nil
}

// 只在快照已确认所属表没有漂移后，从全规则检查中跳过自己的规则。
func withoutOwnedTable(rules string, applied bool) string {
	if !applied {
		return rules
	}
	var out []string
	depth := 0
	skipping := false
	for _, line := range strings.Split(rules, "\n") {
		f := strings.Fields(line)
		if !skipping && len(f) >= 3 && f[0] == "table" && f[1] == "inet" && strings.Trim(f[2], "\"") == TableName {
			skipping = true
		}
		if skipping {
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if depth <= 0 {
				skipping = false
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// full 模式仍使用旧字段的 JSON 顺序和默认值计算指纹，已有活动快照可直接验证。
func configDigest(c Config) string {
	var value any = c
	if c.AccessMode == "full" {
		value = struct {
			AccessMode           *string `json:"access_mode,omitempty"`
			FakeIPRange          *string `json:"fakeip_range,omitempty"`
			DNSSource            *string `json:"dns_source,omitempty"`
			StaticRouteConfirmed *bool   `json:"static_route_confirmed,omitempty"`
			Config
		}{Config: c}
	}
	b, _ := json.Marshal(value)
	return digest(string(b))
}

func hasDNSMark(rules string) bool {
	for _, line := range strings.Split(rules, "\n") {
		if !strings.Contains(strings.ToLower(line), "mark") {
			continue
		}
		for _, token := range strings.Fields(line) {
			token = strings.Trim(token, "{}(),;")
			token = strings.Split(token, "/")[0]
			if value, e := strconv.ParseUint(token, 0, 32); e == nil && value == 0x5342 {
				return true
			}
		}
	}
	return false
}
