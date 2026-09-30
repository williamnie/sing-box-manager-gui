package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

type Plan struct {
	Config       Config            `json:"config"`
	ConfigDigest string            `json:"config_digest"`
	NFTables     string            `json:"nftables"`
	Sysctls      map[string]string `json:"sysctls"`
	DHCPConfig   string            `json:"dhcp_config,omitempty"`
	Warnings     []string          `json:"warnings"`
	PolicyRoutes []string          `json:"policy_routes,omitempty"`
}

type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}
type CheckResult struct {
	Ready    bool      `json:"ready"`
	Findings []Finding `json:"findings"`
	Plan     Plan      `json:"plan"`
}
type Status struct {
	Rebooted         bool      `json:"rebooted"`
	ManagerService   string    `json:"manager_service"`
	Applied          bool      `json:"applied"`
	Drift            bool      `json:"drift"`
	RecoveryRequired bool      `json:"recovery_required"`
	ConfigDigest     string    `json:"config_digest,omitempty"`
	AppliedAt        string    `json:"applied_at,omitempty"`
	Findings         []Finding `json:"findings"`
}

// Preview 仅生成文本，不执行命令，也不会更改操作系统。
func Preview(role string, config Config) (Plan, error) {
	c := Normalize(config)
	if role != "gateway" {
		return Plan{}, fmt.Errorf("仅 gateway 部署角色可生成网关计划")
	}
	if err := Validate(c); err != nil {
		return Plan{}, err
	}
	if c.AccessMode == "dns" {
		return previewDNS(c), nil
	}
	p := Plan{Config: c, ConfigDigest: configDigest(c), Sysctls: map[string]string{
		"net.ipv4.ip_forward":                                 "1",
		"net.ipv4.conf.all.send_redirects":                    "0",
		"net/ipv4/conf/" + c.LANInterface + "/send_redirects": "0",
		"net.ipv4.conf.all.rp_filter":                         "0",
		"net/ipv4/conf/" + c.LANInterface + "/rp_filter":      "2",
		"net/ipv4/conf/" + c.UplinkInterface + "/rp_filter":   "2",
	}, Warnings: []string{"客户端的 IPv4/IPv6 默认路由必须实际经过此设备；DNS/FakeIP 不证明接管完整", "本辅助服务不写 sing-box 的 TUN、策略路由及自动重定向表；这些资源由 sing-box 管理", "先迁移一台设备；保留原主路由/DNS 和带外管理通道"}}
	if c.IPv6Mode == "proxy" {
		p.Sysctls["net.ipv6.conf.all.forwarding"] = "1"
		p.Sysctls["net/ipv6/conf/"+c.UplinkInterface+"/accept_ra"] = "2"
	} else {
		p.Warnings = append(p.Warnings, "仅拒绝经过本机 LAN 的 IPv6 转发；客户端经主路由直出的 IPv6 必须在客户端/主路由另行处理")
	}
	if c.NAT {
		p.Warnings = append(p.Warnings, "已显式启用 IPv4 源 NAT：会隐藏客户端来源，仅用于上游缺少回程路由的拓扑")
	} else {
		p.Warnings = append(p.Warnings, "未启用 NAT：上游必须具有回到客户端 LAN 网段的路由")
	}
	v4 := []string{}
	v6 := []string{}
	for _, s := range c.LANCIDRs {
		if netip.MustParsePrefix(s).Addr().Is4() {
			v4 = append(v4, s)
		} else {
			v6 = append(v6, s)
		}
	}
	excluded4, excluded6 := append([]string{}, v4...), append([]string{}, v6...)
	for _, s := range c.ExcludeCIDRs {
		if netip.MustParsePrefix(s).Addr().Is4() {
			excluded4 = append(excluded4, s)
		} else {
			excluded6 = append(excluded6, s)
		}
	}
	var bld strings.Builder
	fmt.Fprintf(&bld, "table inet %s {\n comment \"singbox-manager gateway owned v1\"\n chain dns_access {\n  type filter hook input priority -10; policy accept;\n", TableName)
	bld.WriteString("  iifname \"lo\" return\n")
	fmt.Fprintf(&bld, "  ip daddr %s meta l4proto { tcp, udp } th dport %d iifname != \"%s\" counter drop\n", c.LANAddress, c.DNSPort, c.LANInterface)
	fmt.Fprintf(&bld, "  ip daddr %s meta l4proto { tcp, udp } th dport %d ip saddr != { %s } counter drop\n", c.LANAddress, c.DNSPort, strings.Join(v4, ", "))
	bld.WriteString(" }\n chain lan_forward {\n  type filter hook forward priority -10; policy accept;\n")
	for _, s := range c.BypassCIDRs {
		proto := "ip"
		if netip.MustParsePrefix(s).Addr().Is6() {
			proto = "ip6"
		}
		fmt.Fprintf(&bld, "  iifname \"%s\" %s saddr %s return\n", c.LANInterface, proto, s)
	}
	fmt.Fprintf(&bld, "  iifname \"%s\" ip saddr { %s } ip daddr != { %s } oifname != \"%s\" counter reject with icmp type admin-prohibited\n", c.LANInterface, strings.Join(v4, ", "), strings.Join(excluded4, ", "), TUNInterface)
	if c.IPv6Mode == "proxy" {
		fmt.Fprintf(&bld, "  iifname \"%s\" ip6 saddr { %s } ip6 daddr != { %s } oifname != \"%s\" counter reject with icmpv6 type admin-prohibited\n", c.LANInterface, strings.Join(v6, ", "), strings.Join(excluded6, ", "), TUNInterface)
	}
	if c.IPv6Mode == "disabled" {
		fmt.Fprintf(&bld, "  iifname \"%s\" meta nfproto ipv6 counter reject with icmpv6 type admin-prohibited\n", c.LANInterface)
	}
	bld.WriteString(" }\n")
	if c.NAT {
		fmt.Fprintf(&bld, " chain optional_nat {\n  type nat hook postrouting priority srcnat; policy accept;\n  iifname \"%s\" oifname \"%s\" ip saddr { %s } ip daddr != { %s } masquerade\n }\n", c.LANInterface, c.UplinkInterface, strings.Join(v4, ", "), strings.Join(excluded4, ", "))
	}
	bld.WriteString("}\n")
	p.NFTables = bld.String()
	if c.DHCP.Enabled {
		p.DHCPConfig = GenerateDHCP(c)
		p.Warnings = append(p.Warnings, "DHCP 已显式启用；端口检查不能发现同广播域另一台服务器，必须先人工确认 DHCP 唯一性")
	}
	return p, nil
}

// GenerateDHCP 复用 dnsmasq DHCP，不启用它的 DNS，也不读取系统 dnsmasq 配置。
func GenerateDHCP(c Config) string {
	c = Normalize(c)
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by singbox-manager; DHCP only\nport=0\ninterface=%s\nbind-interfaces\n", c.LANInterface)
	fmt.Fprintf(&b, "dhcp-range=%s,%s,%s\ndhcp-option=option:router,%s\ndhcp-option=option:dns-server,%s\n", c.DHCP.RangeStart, c.DHCP.RangeEnd, c.DHCP.LeaseTime, c.LANAddress, c.LANAddress)
	for i, r := range c.DHCP.Reservations {
		mac, _ := net.ParseMAC(r.MAC)
		fmt.Fprintf(&b, "dhcp-host=%s,set:device%d", mac.String(), i)
		if r.Group != "" {
			fmt.Fprintf(&b, ",set:group_%s", r.Group)
		}
		fmt.Fprintf(&b, ",%s", r.Address)
		if r.Hostname != "" {
			fmt.Fprintf(&b, ",%s", r.Hostname)
		}
		b.WriteByte('\n')
		if r.Gateway != "" {
			fmt.Fprintf(&b, "dhcp-option=tag:device%d,option:router,%s\n", i, r.Gateway)
		}
	}
	return b.String()
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
