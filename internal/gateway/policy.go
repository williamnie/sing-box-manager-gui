package gateway

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"syscall"
)

// Policy 来自 root 所有且非组/其他用户可写的文件，不能经管理 API 修改。
type Policy struct {
	ManagedDataDir          string   `json:"managed_data_dir,omitempty"`
	AllowDNSBypass          bool     `json:"allow_dns_bypass"`
	AllowRoutedTraffic      bool     `json:"allow_routed_traffic"`
	AllowGateway            bool     `json:"allow_gateway"`
	AllowedLANInterfaces    []string `json:"allowed_lan_interfaces"`
	AllowedUplinkInterfaces []string `json:"allowed_uplink_interfaces"`
	AllowedLANCIDRs         []string `json:"allowed_lan_cidrs"`
	AllowDHCP               bool     `json:"allow_dhcp"`
	AllowNAT                bool     `json:"allow_nat"`
	ManagedExecutable       string   `json:"managed_executable"`
	ManagedConfig           string   `json:"managed_config"`
}

func LoadPolicy(path string) (Policy, error) {
	if err := trustedPath(path, false); err != nil {
		return Policy{}, err
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return Policy{}, e
	}
	var p Policy
	if e = json.Unmarshal(b, &p); e != nil {
		return Policy{}, fmt.Errorf("辅助服务策略格式无效")
	}
	if p.ManagedDataDir == "" {
		p.ManagedDataDir = filepath.Dir(filepath.Dir(p.ManagedConfig))
	}
	if !filepath.IsAbs(p.ManagedDataDir) || !filepath.IsAbs(p.ManagedExecutable) || !filepath.IsAbs(p.ManagedConfig) {
		return Policy{}, fmt.Errorf("辅助服务策略中的实例路径必须为绝对路径")
	}
	return p, nil
}
func trustedPath(path string, directory bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("辅助服务路径必须为绝对路径")
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("辅助服务路径必须为 root 所有且不可由组/其他用户写入: %s", p)
		}
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 {
			return fmt.Errorf("辅助服务路径不是 root 所有: %s", p)
		}
		if p == path && directory && !st.IsDir() {
			return fmt.Errorf("辅助服务状态路径不是目录")
		}
		if p == "/" {
			break
		}
	}
	return nil
}
func (p Policy) authorize(role string, c Config) error {
	if role != "gateway" {
		return fmt.Errorf("须显式选择 gateway 角色并启用网关后才能更改系统")
	}
	if Normalize(c).AccessMode == "dns" && !p.AllowDNSBypass {
		return fmt.Errorf("root 辅助服务策略尚未允许 DNS 分流旁路（allow_dns_bypass）")
	}
	if c.CaptureRoutedTraffic && !p.AllowRoutedTraffic {
		return fmt.Errorf("root 辅助服务策略尚未允许接收主路由转交的公网流量（allow_routed_traffic）")
	}
	if !p.AllowGateway {
		return fmt.Errorf("root 辅助服务策略尚未允许网关操作")
	}
	if !includes(p.AllowedLANInterfaces, c.LANInterface) || !includes(p.AllowedUplinkInterfaces, c.UplinkInterface) {
		return fmt.Errorf("接口不在 root 策略允许范围")
	}
	for _, s := range c.LANCIDRs {
		cidr, e := netip.ParsePrefix(s)
		if e != nil {
			return e
		}
		allowed := false
		for _, v := range p.AllowedLANCIDRs {
			a, e := netip.ParsePrefix(v)
			if e == nil && a.Addr().BitLen() == cidr.Addr().BitLen() && a.Bits() <= cidr.Bits() && a.Contains(cidr.Addr()) {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("LAN 网段不在 root 策略允许范围")
		}
	}
	if c.NAT && !p.AllowNAT {
		return fmt.Errorf("root 策略尚未允许 NAT")
	}
	if c.DHCP.Enabled && !p.AllowDHCP {
		return fmt.Errorf("root 策略尚未允许 DHCP")
	}
	return nil
}
func includes(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
