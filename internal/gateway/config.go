// Package gateway implements the narrow, opt-in Linux gateway privilege boundary.
package gateway

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
)

const TableName = "sbm_gateway"
const TUNInterface = "sbm-tun"
const DefaultSocket = "/run/sbm-gateway/control.sock"
const TProxyPort = 9898
const DNSRouteTable = 20230
const DNSRulePriority = 12030
const DNSMark = "0x5342"

type Config struct {
	CaptureRoutedTraffic bool       `json:"capture_routed_traffic,omitempty"` // DNS 旁路额外接收主路由转交的公网 IPv4
	AccessMode           string     `json:"access_mode"`                      // full（旧配置）/dns
	FakeIPRange          string     `json:"fakeip_range"`
	DNSSource            string     `json:"dns_source"` // client/router
	StaticRouteConfirmed bool       `json:"static_route_confirmed"`
	BypassCIDRs          []string   `json:"bypass_cidrs"`
	Enabled              bool       `json:"enabled"`
	LANInterface         string     `json:"lan_interface"`
	LANCIDRs             []string   `json:"lan_cidrs"`
	LANAddress           string     `json:"lan_address"`
	UpstreamGateway      string     `json:"upstream_gateway"`
	UplinkInterface      string     `json:"uplink_interface"`
	IPv6Mode             string     `json:"ipv6_mode"`
	NAT                  bool       `json:"nat"`
	ExcludeCIDRs         []string   `json:"exclude_cidrs"`
	DNSPort              int        `json:"dns_port"`
	DHCP                 DHCPConfig `json:"dhcp"`
}

type DHCPConfig struct {
	Enabled      bool              `json:"enabled"`
	RangeStart   string            `json:"range_start"`
	RangeEnd     string            `json:"range_end"`
	LeaseTime    string            `json:"lease_time"`
	Reservations []DHCPReservation `json:"reservations"`
}

type DHCPReservation struct {
	MAC      string `json:"mac"`
	Address  string `json:"address"`
	Hostname string `json:"hostname"`
	Group    string `json:"group"`
	Gateway  string `json:"gateway"`
}

var interfacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,14}$`)
var labelPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)
var leasePattern = regexp.MustCompile(`^[1-9][0-9]{0,5}[mhd]$`)

func Normalize(c Config) Config {
	if c.AccessMode == "" {
		c.AccessMode = "full"
	}
	if c.FakeIPRange == "" {
		c.FakeIPRange = "198.18.0.0/15"
	}
	if c.DNSSource == "" {
		c.DNSSource = "client"
	}
	if c.IPv6Mode == "" {
		c.IPv6Mode = "disabled"
	}
	if c.DNSPort == 0 {
		c.DNSPort = 53
	}
	if c.UplinkInterface == "" {
		c.UplinkInterface = c.LANInterface
	}
	if c.DHCP.LeaseTime == "" {
		c.DHCP.LeaseTime = "12h"
	}
	return c
}

func Validate(c Config) error {
	c = Normalize(c)
	if c.AccessMode != "full" && c.AccessMode != "dns" {
		return fmt.Errorf("接入方式必须为 full 或 dns")
	}
	if c.CaptureRoutedTraffic && c.AccessMode != "dns" {
		return fmt.Errorf("接收主路由转交的公网流量仅适用于 DNS 分流旁路")
	}
	if c.AccessMode == "dns" {
		if c.IPv6Mode != "disabled" || c.NAT || c.DHCP.Enabled {
			return fmt.Errorf("DNS 分流旁路仅支持 IPv4 FakeIP，不能启用 IPv6 接管、NAT 或 DHCP")
		}
		if c.DNSSource != "client" && c.DNSSource != "router" {
			return fmt.Errorf("DNS 来源必须为 client 或 router")
		}
		p, e := netip.ParsePrefix(c.FakeIPRange)
		reserved := netip.MustParsePrefix("198.18.0.0/15")
		if e != nil || !p.Addr().Is4() || p != p.Masked() || p.Bits() < reserved.Bits() || p.Bits() > 24 || !reserved.Contains(p.Addr()) {
			return fmt.Errorf("FakeIP 地址池须为 198.18.0.0/15 内的规范 /15 至 /24 子网")
		}
		for _, cidr := range append(append([]string{}, c.LANCIDRs...), c.ExcludeCIDRs...) {
			if other, e := netip.ParsePrefix(cidr); e == nil && p.Overlaps(other) {
				return fmt.Errorf("FakeIP 地址池不能与 LAN 或排除网段重叠")
			}
		}
		if c.DNSPort != 53 {
			return fmt.Errorf("DNS 分流旁路需要标准 LAN DNS 53 端口")
		}
	}
	if !interfacePattern.MatchString(c.LANInterface) || !interfacePattern.MatchString(c.UplinkInterface) {
		return fmt.Errorf("LAN 和上游接口名称无效")
	}
	if c.IPv6Mode != "disabled" && c.IPv6Mode != "proxy" {
		return fmt.Errorf("IPv6 模式只能为 disabled 或 proxy")
	}
	if c.DNSPort < 1 || c.DNSPort > 65535 {
		return fmt.Errorf("DNS 端口超出范围")
	}
	lan, err := netip.ParseAddr(c.LANAddress)
	if err != nil || !lan.Is4() || lan.IsUnspecified() || lan.IsLoopback() || lan.IsMulticast() {
		return fmt.Errorf("LAN 地址必须为接口上的单播 IPv4 地址")
	}
	gw, err := netip.ParseAddr(c.UpstreamGateway)
	if err != nil || !gw.Is4() || gw.IsUnspecified() || gw.IsMulticast() || gw == lan {
		return fmt.Errorf("上游网关必须为不同的单播 IPv4 地址")
	}
	if len(c.LANCIDRs) == 0 || len(c.LANCIDRs) > 32 || len(c.ExcludeCIDRs) > 128 {
		return fmt.Errorf("LAN 网段必填且最多 32 项，排除网段最多 128 项")
	}
	var containsLAN, hasIPv6 bool
	for _, s := range c.LANCIDRs {
		p, e := netip.ParsePrefix(s)
		if e != nil || p != p.Masked() || p.Bits() == 0 || p.Addr().IsLoopback() || p.Addr().IsMulticast() {
			return fmt.Errorf("LAN 网段必须为规范、非默认网段: %s", s)
		}
		containsLAN = containsLAN || p.Contains(lan)
		hasIPv6 = hasIPv6 || p.Addr().Is6()
	}
	if !containsLAN {
		return fmt.Errorf("LAN 地址不在 LAN 网段内")
	}
	if c.IPv6Mode == "proxy" && !hasIPv6 {
		return fmt.Errorf("IPv6 代理需要显式填写实际 LAN IPv6 前缀")
	}
	for _, s := range c.ExcludeCIDRs {
		if p, e := netip.ParsePrefix(s); e != nil || p != p.Masked() || p.Bits() == 0 {
			return fmt.Errorf("无效排除网段: %s", s)
		}
	}
	if len(c.BypassCIDRs) > 512 {
		return fmt.Errorf("绕过来源最多 512 项")
	}
	for _, v := range c.BypassCIDRs {
		p, e := netip.ParsePrefix(v)
		if e != nil || p != p.Masked() {
			return fmt.Errorf("绕过来源必须为规范 CIDR")
		}
		ok := false
		for _, lan := range c.LANCIDRs {
			l := netip.MustParsePrefix(lan)
			if l.Addr().BitLen() == p.Addr().BitLen() && l.Bits() <= p.Bits() && l.Contains(p.Addr()) {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("绕过来源须位于 LAN 网段内")
		}
	}
	if !c.DHCP.Enabled {
		return nil
	}
	if c.DNSPort != 53 {
		return fmt.Errorf("DHCP 客户端需要标准 DNS 53 端口")
	}
	start, e1 := netip.ParseAddr(c.DHCP.RangeStart)
	end, e2 := netip.ParseAddr(c.DHCP.RangeEnd)
	if e1 != nil || e2 != nil || !start.Is4() || !end.Is4() || start.Compare(end) > 0 || !sameLAN(c, start, end) {
		return fmt.Errorf("DHCP 地址池须位于同一 LAN IPv4 网段且起止顺序有效")
	}
	if start.Compare(lan) <= 0 && lan.Compare(end) <= 0 || start.Compare(gw) <= 0 && gw.Compare(end) <= 0 {
		return fmt.Errorf("DHCP 地址池不能包含本机或上游网关")
	}
	if !leasePattern.MatchString(c.DHCP.LeaseTime) {
		return fmt.Errorf("DHCP 租期格式应为 12h、30m 或 1d")
	}
	if len(c.DHCP.Reservations) > 512 {
		return fmt.Errorf("DHCP 保留地址最多 512 项")
	}
	macs, ips := map[string]bool{}, map[string]bool{}
	for _, r := range c.DHCP.Reservations {
		mac, e := net.ParseMAC(r.MAC)
		a, ae := netip.ParseAddr(r.Address)
		if e != nil || len(mac) != 6 || ae != nil || !a.Is4() || !sameLAN(c, lan, a) || a == lan || a == gw {
			return fmt.Errorf("DHCP 保留地址或 MAC 无效")
		}
		if macs[strings.ToLower(r.MAC)] || ips[r.Address] {
			return fmt.Errorf("DHCP 保留地址或 MAC 重复")
		}
		macs[strings.ToLower(r.MAC)] = true
		ips[r.Address] = true
		if r.Hostname != "" && !labelPattern.MatchString(r.Hostname) || r.Group != "" && !labelPattern.MatchString(r.Group) {
			return fmt.Errorf("DHCP 主机名或分组标识无效")
		}
		if r.Gateway != "" {
			a, e := netip.ParseAddr(r.Gateway)
			if e != nil || !sameLAN(c, lan, a) {
				return fmt.Errorf("DHCP 设备网关必须在 LAN 内")
			}
		}
	}
	return nil
}

func sameLAN(c Config, a, b netip.Addr) bool {
	for _, s := range c.LANCIDRs {
		p, e := netip.ParsePrefix(s)
		if e == nil && p.Contains(a) && p.Contains(b) {
			return true
		}
	}
	return false
}
