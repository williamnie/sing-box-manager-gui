package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"
)

const ipv6SetDeclaration = " set ipv6_lan {\n  type ipv6_addr; flags interval;\n"

type IPv6Status struct {
	Prefixes   []string `json:"prefixes"`
	Generation uint64   `json:"generation"`
	UpdatedAt  string   `json:"updated_at"`
	Error      string   `json:"error,omitempty"`
}

// 仅地址和直连路由相互印证的 LAN 子网可成为目的例外；地址本身不是可信来源证明。
// 不推断 DHCPv6-PD、远端路由、设备身份，也不扫描其他接口。
func (m *Manager) discoverIPv6(ctx context.Context, c Config) ([]string, error) {
	c = Normalize(c)
	if e := m.Policy.authorize("gateway", c); e != nil {
		return nil, e
	}
	var within []netip.Prefix
	for _, value := range m.Policy.DynamicIPv6Within {
		p, e := netip.ParsePrefix(value)
		if e != nil || !p.Addr().Is6() || p != p.Masked() || p.Bits() == 0 {
			return nil, fmt.Errorf("root 策略 dynamic_ipv6_within 包含无效 IPv6 范围")
		}
		within = append(within, p)
	}
	addresses, e := m.run(ctx, "ip", "-j", "address", "show", "dev", c.LANInterface)
	if e != nil {
		return nil, fmt.Errorf("无法读取 LAN IPv6 地址")
	}
	routes, e := m.run(ctx, "ip", "-j", "-6", "route", "show", "table", "main", "dev", c.LANInterface)
	if e != nil {
		return nil, fmt.Errorf("无法读取 LAN IPv6 直连路由")
	}
	var interfaces []interfaceAddresses
	var entries []struct {
		Dst      string          `json:"dst"`
		Dev      string          `json:"dev"`
		Gateway  string          `json:"gateway"`
		Type     string          `json:"type"`
		Protocol string          `json:"protocol"`
		Flags    []string        `json:"flags"`
		Expires  json.RawMessage `json:"expires"`
		Nexthops json.RawMessage `json:"nexthops"`
		NHID     int             `json:"nhid"`
	}
	if json.Unmarshal([]byte(addresses), &interfaces) != nil || json.Unmarshal([]byte(routes), &entries) != nil || len(interfaces) != 1 || len(entries) > 256 {
		return nil, fmt.Errorf("LAN IPv6 地址或路由输出无效或超出数量限制")
	}
	if interfaces[0].IfName != "" && interfaces[0].IfName != c.LANInterface {
		return nil, fmt.Errorf("IPv6 地址不属于已授权 LAN 接口")
	}
	found := map[string]bool{}
	for _, route := range entries {
		// iproute2 在显式 dev 过滤时可能省略每条记录的 dev，过滤参数已固定为授权接口。
		p, err := netip.ParsePrefix(route.Dst)
		if err != nil || !p.Addr().Is6() || !p.Addr().IsGlobalUnicast() || p != p.Masked() || p.Bits() < 48 || p.Bits() > 64 || route.Dev != "" && route.Dev != c.LANInterface || route.Gateway != "" || route.NHID != 0 || len(route.Nexthops) > 0 && string(route.Nexthops) != "null" && string(route.Nexthops) != "[]" || route.Type != "" && route.Type != "unicast" || route.Protocol != "kernel" && route.Protocol != "ra" || includes(route.Flags, "linkdown") || includes(route.Flags, "dead") || expiredIPv6Lifetime(route.Expires) {
			continue
		}
		if p.Addr().IsPrivate() && !m.Policy.DynamicIPv6AllowULA {
			continue
		}
		allowed := len(within) == 0
		for _, scope := range within {
			allowed = allowed || scope.Bits() <= p.Bits() && scope.Contains(p.Addr())
		}
		if !allowed {
			continue
		}
		for _, address := range interfaces[0].AddrInfo {
			a, err := netip.ParseAddr(address.Local)
			// /128 的 DHCPv6 地址需由独立的 on-link 路由提供真实子网长度。
			if err != nil || !a.Is6() || !p.Contains(a) || address.PrefixLen != p.Bits() && address.PrefixLen != 128 || address.Scope != "" && address.Scope != "global" || address.Tentative || address.DADFailed || includes(address.Flags, "tentative") || includes(address.Flags, "dadfailed") || expiredIPv6Lifetime(address.ValidLifeTime) {
				continue
			}
			// deprecated/preferred_lft=0 不等于失效，现存连接仍可能使用它。
			found[p.String()] = true
		}
	}
	if len(found) > 32 {
		return nil, fmt.Errorf("LAN IPv6 前缀超过 32 项，拒绝扩大目的例外")
	}
	prefixes := make([]string, 0, len(found))
	for prefix := range found {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	// 重叠的委派聚合网段不能作为整段直连例外，也不能生成冲突的 interval set。
	for i, value := range prefixes {
		p := netip.MustParsePrefix(value)
		for _, other := range prefixes[i+1:] {
			if p.Overlaps(netip.MustParsePrefix(other)) {
				return nil, fmt.Errorf("LAN IPv6 存在重叠前缀，需要明确直连子网拓扑")
			}
		}
	}
	return prefixes, nil
}

func expiredIPv6Lifetime(value json.RawMessage) bool {
	if len(value) == 0 || string(value) == `"forever"` {
		return false
	}
	var seconds float64
	return json.Unmarshal(value, &seconds) != nil || seconds <= 0
}

func withIPv6Prefixes(p Plan, prefixes []string) Plan {
	p.IPv6Prefixes = append([]string{}, prefixes...)
	if len(prefixes) != 0 {
		p.NFTables = strings.Replace(p.NFTables, ipv6SetDeclaration, ipv6SetDeclaration+"  elements = { "+strings.Join(prefixes, ", ")+" }\n", 1)
	}
	return p
}

func ipv6SetUpdate(prefixes []string) string {
	script := "flush set inet " + TableName + " ipv6_lan\n"
	if len(prefixes) != 0 {
		script += "add element inet " + TableName + " ipv6_lan { " + strings.Join(prefixes, ", ") + " }\n"
	}
	return script
}

func ipv6Template(p Plan) string {
	if len(p.IPv6Prefixes) == 0 {
		return p.NFTables
	}
	return strings.Replace(p.NFTables, "  elements = { "+strings.Join(p.IPv6Prefixes, ", ")+" }\n", "", 1)
}

// 动态集合的失败补偿也只改该集合，不能顺带重启 DHCP 或回写 sysctl。
func (m *Manager) restoreIPv6Pending(ctx context.Context, s *state, live snapshot) error {
	tx := s.Pending
	if s.Active == nil {
		return fmt.Errorf("IPv6 同步事务缺少原活动配置，拒绝猜测恢复")
	}
	otherResources := live
	otherResources.NFT = tx.Before.NFT
	if !equalSnapshot(otherResources, tx.Before) {
		return fmt.Errorf("IPv6 同步期间其他网关资源被修改，拒绝覆盖")
	}
	if live.NFT != tx.Before.NFT {
		now, e := m.nft(ctx)
		if e != nil || now != live.NFT {
			return fmt.Errorf("恢复 IPv6 集合前 nftables 已改变")
		}
		if _, e := m.Runner.Run(ctx, "nft", []string{"-f", "-"}, ipv6SetUpdate(s.Active.Plan.IPv6Prefixes)); e != nil {
			return e
		}
		now, e = m.nft(ctx)
		if e != nil || now != tx.Before.NFT {
			return fmt.Errorf("恢复后的 IPv6 资源指纹不匹配，保留事务待核对")
		}
	}
	s.Pending = nil
	return m.save(*s)
}

// SyncIPv6 只刷新 helper 已应用配置的运行态，不接收 Web 提供的前缀。
// 发现失败时清空动态目的例外，接口级断核保护始终保留。
func (m *Manager) SyncIPv6(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.syncIPv6(ctx)
	if e != nil {
		m.ipv6Error = e.Error()
	} else {
		m.ipv6Error = ""
	}
	return e
}

func (m *Manager) syncIPv6(ctx context.Context) error {
	if m.GOOS != "linux" {
		return nil
	}
	s, e := m.load()
	if e != nil || s.Active == nil || !s.Active.Plan.Config.AutoIPv6() {
		return e
	}
	if _, _, finding := m.bootBoundary(s); finding != nil || s.Pending != nil {
		return fmt.Errorf("自动 IPv6 同步暂停：存在旧启动状态或未完成事务，请先恢复")
	}
	// 先做只读的前缀发现（两次 ip 查询）；前缀未变且近期完整校验通过时不再采集整套 nft/sysctl。
	prefixes, discoverErr := m.discoverIPv6(ctx, s.Active.Plan.Config)
	if discoverErr != nil {
		prefixes = []string{}
	}
	key := fmt.Sprintf("%s|%s|%d", s.Active.Plan.ConfigDigest, s.Active.AppliedAt, s.Active.IPv6Generation)
	if slices.Equal(prefixes, s.Active.Plan.IPv6Prefixes) && m.ipv6VerifiedKey == key && time.Since(m.ipv6VerifiedAt) < ipv6VerifyInterval {
		return discoverErr
	}
	m.ipv6VerifiedKey = ""
	current, e := m.capture(ctx, s.Active.Plan)
	if e != nil {
		return e
	}
	if !equalSnapshot(current, s.Active.Current) {
		return fmt.Errorf("自动 IPv6 同步暂停：所属资源被外部修改，拒绝覆盖")
	}
	plan, e := Preview("gateway", s.Active.Plan.Config)
	if e != nil {
		return e
	}
	if ipv6Template(plan) != ipv6Template(s.Active.Plan) {
		return fmt.Errorf("自动 IPv6 规则版本发生变化，请先恢复网关并重新检查、应用")
	}
	if !slices.Equal(prefixes, s.Active.Plan.IPv6Prefixes) {
		plan = withIPv6Prefixes(plan, prefixes)
		plan.Sysctls = maps.Clone(s.Active.Plan.Sysctls)
		if _, e = m.Runner.Run(ctx, "nft", []string{"--check", "-f", "-"}, ipv6SetUpdate(prefixes)); e != nil {
			return fmt.Errorf("IPv6 前缀集合未通过 nftables 校验: %w", e)
		}
		if _, e = m.applyPlan(ctx, s, plan); e != nil {
			return e
		}
		// 写入后状态标识已变化，下一轮重新完整校验。
		return discoverErr
	}
	m.ipv6VerifiedKey, m.ipv6VerifiedAt = key, time.Now()
	return discoverErr
}

// 前缀不变时完整漂移校验的最长间隔；外部修改仍会由 Status 实时检测并阻止写入。
const ipv6VerifyInterval = 5 * time.Minute

// 生命周期由 helper 的监听服务管理；重启主机后的旧状态绝不自动重放。
func (m *Manager) RunIPv6Sync(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		check, cancel := context.WithTimeout(ctx, 20*time.Second)
		_ = m.SyncIPv6(check)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
