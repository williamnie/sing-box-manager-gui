package storage

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/gateway"
)

// DeviceGroup 是显式配置的策略，不通过操作系统推断部署角色。
type DeviceGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Policy   string `json:"policy"` // split/direct/strict/bypass
	Outbound string `json:"outbound,omitempty"`
}
type Device struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	GroupID   string   `json:"group_id"`
	Enabled   bool     `json:"enabled"`
}
type SplitDNSRule struct {
	ID           string   `json:"id"`
	DomainSuffix []string `json:"domain_suffix"`
	SourceCIDRs  []string `json:"source_cidrs,omitempty"`
	Server       string   `json:"server"` // direct/proxy
}

// ImportedPolicy 只保存经预览确认的策略；不保留原实例的端口、路由接管或控制接口。
type ImportedPolicy struct {
	Outbounds  []map[string]any `json:"outbounds"`
	Rules      []map[string]any `json:"rules"`
	RuleSets   []map[string]any `json:"rule_sets"`
	DNS        map[string]any   `json:"dns,omitempty"`
	Final      string           `json:"final"`
	SourceHash string           `json:"source_hash"`
}

func NormalizeSettings(s *Settings) {
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	if s.DeploymentRole == "" {
		s.DeploymentRole = "desktop"
	}
	s.Gateway = gateway.Normalize(s.Gateway)
	s.Gateway.BypassCIDRs = nil
	groups := map[string]string{}
	for _, g := range s.DeviceGroups {
		groups[g.ID] = g.Policy
	}
	for _, d := range EffectiveDevices(s) {
		if d.Enabled && groups[d.GroupID] == "bypass" {
			for _, a := range d.Addresses {
				if p, err := SourcePrefix(a); err == nil {
					s.Gateway.BypassCIDRs = append(s.Gateway.BypassCIDRs, p.Masked().String())
				}
			}
		}
	}
	sort.Strings(s.Gateway.BypassCIDRs)
	if s.Gateway.IPv6Mode == "" {
		s.Gateway.IPv6Mode = "disabled"
	}
}

func SourcePrefix(s string) (netip.Prefix, error) {
	if ip, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(ip, ip.BitLen()), nil
	}
	return netip.ParsePrefix(s)
}

var domainPattern = regexp.MustCompile(`^[a-zA-Z0-9_*.-]+$`)

func ValidateSettings(s *Settings) error {
	switch s.LogLevel {
	case "", "trace", "debug", "info", "warn", "error", "fatal", "panic":
	default:
		return fmt.Errorf("无效日志级别")
	}
	if s.DeploymentRole != "desktop" && s.DeploymentRole != "gateway" {
		return fmt.Errorf("未知部署角色")
	}
	if s.DeploymentRole != "gateway" && s.Gateway.Enabled {
		return fmt.Errorf("单机模式不能启用网关接管")
	}
	dnsBypass := s.DeploymentRole == "gateway" && gateway.Normalize(s.Gateway).AccessMode == "dns"
	if dnsBypass {
		if err := gateway.Validate(s.Gateway); err != nil {
			return err
		}
		for _, p := range []int{s.MixedPort, s.WebPort, s.ClashAPIPort} {
			if p == 53 || p == gateway.TProxyPort {
				return fmt.Errorf("DNS 旁路保留端口 53 和 %d，不能与其他监听重复", gateway.TProxyPort)
			}
		}
	}
	// Web API 不允许重定向写文件或更换可执行程序。
	if s.SingBoxPath != "bin/sing-box" || s.ConfigPath != "generated/config.json" {
		return fmt.Errorf("内核和配置路径必须位于管理器专属数据目录")
	}
	if s.MixedPort < 1 || s.MixedPort > 65535 || s.ClashAPIPort < 0 || s.ClashAPIPort > 65535 || s.WebPort < 1 || s.WebPort > 65535 {
		return fmt.Errorf("端口必须在有效范围内")
	}
	if s.MixedPort == s.WebPort || s.MixedPort == s.ClashAPIPort || s.WebPort == s.ClashAPIPort {
		return fmt.Errorf("管理、代理和 Clash 端口不能重复")
	}
	groups := map[string]DeviceGroup{}
	for _, g := range s.DeviceGroups {
		if dnsBypass && g.Policy == "strict" {
			return fmt.Errorf("DNS 分流旁路无法保证严格全代理覆盖，请选择普通分流/直连/绕过或完整网关接管")
		}
		if g.ID == "" || groups[g.ID].ID != "" {
			return fmt.Errorf("设备组 ID 为空或重复")
		}
		if g.Policy != "split" && g.Policy != "direct" && g.Policy != "strict" && g.Policy != "bypass" {
			return fmt.Errorf("无效设备组策略")
		}
		if g.Policy == "strict" && (g.Outbound == "DIRECT" || g.Outbound == "REJECT") {
			return fmt.Errorf("严格代理必须选择代理出站")
		}
		groups[g.ID] = g
	}
	var prefixes []netip.Prefix
	ids := map[string]bool{}
	for _, d := range EffectiveDevices(s) {
		if dnsBypass && s.Gateway.DNSSource == "router" && d.Enabled {
			return fmt.Errorf("主路由转发 DNS 会合并客户端来源，不能启用设备分组；请由 DHCP 直接下发旁路 DNS")
		}
		if d.ID == "" || ids[d.ID] {
			return fmt.Errorf("设备 ID 为空或重复")
		}
		ids[d.ID] = true
		if _, ok := groups[d.GroupID]; !ok {
			return fmt.Errorf("设备 %s 的分组不存在", d.Name)
		}
		if len(d.Addresses) == 0 {
			return fmt.Errorf("设备必须提供来源 IP 或网段")
		}
		for _, a := range d.Addresses {
			p, err := SourcePrefix(a)
			if err != nil {
				return fmt.Errorf("设备来源地址无效: %s", a)
			}
			if !d.Enabled {
				continue
			}
			if s.DeploymentRole == "gateway" {
				inside := false
				for _, network := range s.Gateway.LANCIDRs {
					lan, e := netip.ParsePrefix(network)
					if e == nil && lan.Contains(p.Addr()) && lan.Bits() <= p.Bits() {
						inside = true
					}
				}
				if !inside {
					return fmt.Errorf("设备来源 %s 不在声明的 LAN 网段内", a)
				}
			}
			for _, prev := range prefixes {
				if p.Overlaps(prev) {
					return fmt.Errorf("启用设备的来源网段重叠: %s", a)
				}
			}
			prefixes = append(prefixes, p)
		}
	}
	for _, r := range s.SplitDNS {
		if dnsBypass && s.Gateway.DNSSource == "router" && len(r.SourceCIDRs) > 0 {
			return fmt.Errorf("主路由转发 DNS 不支持按来源区分的 Split DNS")
		}
		if r.Server != "direct" && r.Server != "proxy" {
			return fmt.Errorf("Split DNS 服务器必须为 direct 或 proxy")
		}
		if len(r.DomainSuffix) == 0 {
			return fmt.Errorf("Split DNS 必须提供域名")
		}
		for _, a := range r.SourceCIDRs {
			if _, err := SourcePrefix(a); err != nil {
				return err
			}
		}
		for _, d := range r.DomainSuffix {
			if !domainPattern.MatchString(d) {
				return fmt.Errorf("Split DNS 域名无效")
			}
		}
	}
	for _, h := range s.Hosts {
		if h.Enabled {
			if !domainPattern.MatchString(h.Domain) || len(h.IPs) == 0 {
				return fmt.Errorf("hosts 条目无效")
			}
			for _, ip := range h.IPs {
				if net.ParseIP(ip) == nil {
					return fmt.Errorf("hosts IP 无效")
				}
			}
		}
	}
	return nil
}

func ValidateRule(r Rule, gateway bool) error {
	allowed := map[string]bool{"domain": true, "domain_suffix": true, "domain_keyword": true, "ip_cidr": true, "geosite": true, "geoip": true, "port": true, "port_range": true, "process_name": true, "match": true}
	if !allowed[r.RuleType] {
		return fmt.Errorf("未知规则类型")
	}
	if gateway && (r.RuleType == "process_name" || len(r.ProcessNames) > 0) {
		return fmt.Errorf("网关无法识别远端设备进程名，请使用来源 IP 和协议等条件")
	}
	if strings.TrimSpace(r.Outbound) == "" {
		return fmt.Errorf("规则必须选择出站")
	}
	for _, v := range r.Values {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("规则值不可为空")
		}
		if (r.RuleType == "domain" || r.RuleType == "domain_suffix") && !domainPattern.MatchString(v) {
			return fmt.Errorf("域名条件无效")
		}
	}
	if r.RuleType != "match" && len(r.Values) == 0 {
		return fmt.Errorf("规则条件不可为空")
	}
	if r.RuleType == "match" && len(r.SourceCIDRs)+len(r.Network)+len(r.Protocol)+len(r.Ports)+len(r.PortRanges)+len(r.ProcessNames) == 0 {
		return fmt.Errorf("联合匹配至少需要一个条件")
	}
	for _, s := range r.SourceCIDRs {
		if _, err := SourcePrefix(s); err != nil {
			return fmt.Errorf("来源地址无效: %s", s)
		}
	}
	for _, n := range r.Network {
		if n != "tcp" && n != "udp" {
			return fmt.Errorf("网络协议必须为 tcp 或 udp")
		}
	}
	for _, p := range r.Ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("端口无效")
		}
	}
	ranges := append([]string{}, r.PortRanges...)
	if r.RuleType == "port_range" {
		ranges = append(ranges, r.Values...)
	}
	for _, v := range ranges {
		var a, b int
		if n, _ := fmt.Sscanf(v, "%d:%d", &a, &b); n != 2 || a < 1 || b > 65535 || a > b || fmt.Sprintf("%d:%d", a, b) != v {
			return fmt.Errorf("端口范围必须为 起始:结束")
		}
	}
	if r.RuleType == "port" {
		for _, v := range r.Values {
			var p int
			if n, _ := fmt.Sscanf(v, "%d", &p); n != 1 || p < 1 || p > 65535 || fmt.Sprint(p) != v {
				return fmt.Errorf("端口无效，请使用 port_range 表示范围")
			}
		}
	}
	if r.RuleType == "ip_cidr" {
		for _, v := range r.Values {
			if _, err := SourcePrefix(v); err != nil {
				return err
			}
		}
	}
	return nil
}

func clone[T any](value T) T {
	b, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(b, &result)
	return result
}

// EffectiveDevices 把可选 DHCP 保留地址的分组转为真实来源策略，不依赖远端进程或 DNS 标签。
func EffectiveDevices(s *Settings) []Device {
	devices := append([]Device{}, s.Devices...)
	if !s.Gateway.DHCP.Enabled {
		return devices
	}
	for _, r := range s.Gateway.DHCP.Reservations {
		if r.Group == "" {
			continue
		}
		ip, err := netip.ParseAddr(r.Address)
		if err != nil {
			continue
		}
		covered := false
		for _, d := range s.Devices {
			if !d.Enabled {
				continue
			}
			for _, a := range d.Addresses {
				if p, e := SourcePrefix(a); e == nil && p.Contains(ip) && d.GroupID == r.Group {
					covered = true
				}
			}
		}
		if !covered {
			devices = append(devices, Device{ID: "dhcp-" + r.MAC, Name: "DHCP " + r.Hostname + " " + r.Address, Addresses: []string{r.Address}, GroupID: r.Group, Enabled: true})
		}
	}
	return devices
}
