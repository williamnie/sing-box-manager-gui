package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type snapshot struct {
	DNSRouting dnsRouting        `json:"dns_routing"`
	NFT        string            `json:"nft"`
	Sysctls    map[string]string `json:"sysctls"`
	DHCP       string            `json:"dhcp"`
	DHCPActive bool              `json:"dhcp_active"`
}
type activeState struct {
	Plan      Plan     `json:"plan"`
	Baseline  snapshot `json:"baseline"`
	Current   snapshot `json:"current"`
	AppliedAt string   `json:"applied_at"`
}
type transaction struct {
	ObservedDNSRouting dnsRouting `json:"observed_dns_routing"`
	RollingBack        bool       `json:"rolling_back"`
	Before             snapshot   `json:"before"`
	Target             Plan       `json:"target"`
	ObservedNFT        string     `json:"observed_nft"`
	NFTRecorded        bool       `json:"nft_recorded"`
}
type state struct {
	BootID  string       `json:"boot_id,omitempty"`
	Active  *activeState `json:"active,omitempty"`
	Pending *transaction `json:"pending,omitempty"`
}

type Manager struct {
	Runner     Runner
	Policy     Policy
	StateDir   string
	GOOS       string
	ReadBootID func() (string, error)
	mu         sync.Mutex
}

func NewManager(r Runner, p Policy, stateDir string) *Manager {
	return &Manager{Runner: r, Policy: p, StateDir: stateDir, GOOS: runtime.GOOS, ReadBootID: readLinuxBootID}
}
func (m *Manager) load() (state, error) {
	b, e := os.ReadFile(filepath.Join(m.StateDir, "state.json"))
	if os.IsNotExist(e) {
		return state{}, nil
	}
	if e != nil {
		return state{}, e
	}
	var s state
	e = json.Unmarshal(b, &s)
	return s, e
}
func (m *Manager) save(s state) error {
	if (s.Active != nil || s.Pending != nil) && s.BootID == "" {
		id, e := m.currentBootID()
		if e != nil {
			return e
		}
		s.BootID = id
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(m.StateDir, "state.json"), b)
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".sbm-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(name, path)
	}
	if e == nil {
		if d, de := os.Open(filepath.Dir(path)); de == nil {
			e = d.Sync()
			d.Close()
		} else {
			e = de
		}
	}
	return e
}
func (m *Manager) run(ctx context.Context, name string, args ...string) (string, error) {
	return m.Runner.Run(ctx, name, args, "")
}
func (m *Manager) nft(ctx context.Context) (string, error) {
	out, e := m.run(ctx, "nft", "-s", "list", "tables")
	if e != nil {
		return "", e
	}
	if !strings.Contains(out, "table inet "+TableName) {
		return "", nil
	}
	return m.run(ctx, "nft", "-s", "list", "table", "inet", TableName)
}
func (m *Manager) capture(ctx context.Context, p Plan) (snapshot, error) {
	var s snapshot
	s.Sysctls = map[string]string{}
	var e error
	s.NFT, e = m.nft(ctx)
	if e != nil {
		return s, e
	}
	for _, k := range sortedKeys(p.Sysctls) {
		v, e := m.run(ctx, "sysctl", "-n", k)
		if e != nil {
			return s, e
		}
		s.Sysctls[k] = strings.TrimSpace(v)
	}
	b, e := os.ReadFile(filepath.Join(m.StateDir, "dnsmasq.conf"))
	if e != nil && !os.IsNotExist(e) {
		return s, e
	}
	if p.Config.AccessMode == "dns" {
		s.DNSRouting, e = m.captureDNSRouting(ctx)
		if e != nil {
			return s, e
		}
	}
	s.DHCP = string(b)
	// 未安装 DHCP 服务时 is-active 返回非零，视为未运行。
	out, _ := m.run(ctx, "systemctl", "is-active", "sbm-gateway-dhcp.service")
	s.DHCPActive = strings.TrimSpace(out) == "active"
	return s, nil
}
func (m *Manager) check(ctx context.Context, role string, c Config) (CheckResult, error) {
	p, e := Preview(role, c)
	if e != nil {
		return CheckResult{}, e
	}
	r := CheckResult{Plan: p, Ready: true, Findings: []Finding{}}
	add := func(sev, code, msg string) {
		r.Findings = append(r.Findings, Finding{sev, code, msg})
		if sev == "error" {
			r.Ready = false
		}
	}
	if m.GOOS != "linux" {
		add("error", "platform", "Linux 网关系统操作仅支持 Linux")
		return r, nil
	}
	if e = m.Policy.authorize(role, p.Config); e != nil {
		add("error", "permission", e.Error())
	}
	s, e := m.load()
	if e != nil {
		return r, e
	}
	_, _, bootFinding := m.bootBoundary(s)
	if bootFinding != nil {
		add(bootFinding.Severity, bootFinding.Code, bootFinding.Message)
		return r, nil
	}
	if s.Pending != nil {
		add("error", "pending", "上次事务未完成，请先恢复")
	}
	if p.Config.AccessMode == "dns" && !p.Config.StaticRouteConfirmed {
		add("error", "static_route", "必须先确认主路由已配置 FakeIP 网段指向本机 LAN 地址的静态路由")
	}
	if s.Active != nil && Normalize(s.Active.Plan.Config).AccessMode != p.Config.AccessMode {
		add("error", "access_mode_active", "接入模式切换前须先恢复已应用的系统接管")
		return r, nil
	}
	if s.Active != nil && p.Config.AccessMode == "dns" && (s.Active.Plan.Config.FakeIPRange != p.Config.FakeIPRange || s.Active.Plan.Config.LANInterface != p.Config.LANInterface) {
		add("error", "dns_route_active", "修改 FakeIP 地址池或 LAN 接口前须先恢复接管，停机切换后重新应用")
		return r, nil
	}
	if e = m.completeForwardingPlan(ctx, &p, s.Active); e != nil {
		return r, e
	}
	r.Plan = p
	current, e := m.capture(ctx, p)
	if e != nil {
		return r, e
	}
	if s.Active != nil {
		if e = m.captureExtra(ctx, &current, s.Active.Current.Sysctls); e != nil {
			return r, e
		}
	}
	if s.Active == nil && current.DHCPActive {
		add("error", "dhcp_ownership", "DHCP 服务正在运行但没有所属记录，拒绝接管")
	}
	if s.Active == nil && current.DNSRouting != (dnsRouting{}) {
		add("error", "dns_route_ownership", "DNS 路由表、规则优先级或 mark 已被占用且没有本实例归属记录")
	}
	if s.Active == nil && current.NFT != "" {
		add("error", "ownership", "已存在无所属记录的 sbm_gateway 表，拒绝覆盖")
	}
	if s.Active != nil && !equalSnapshot(current, s.Active.Current) {
		add("error", "drift", "网关资源与上次应用记录不同，拒绝覆盖外部更改")
	}
	if e = m.audit(ctx, p.Config, s.Active != nil, add); e != nil {
		return r, e
	}
	script := replaceNFT(current.NFT, p.NFTables)
	if _, e = m.Runner.Run(ctx, "nft", []string{"--check", "-f", "-"}, script); e != nil {
		add("error", "nft_syntax", "nftables 候选配置未通过系统校验")
	}
	if p.Config.DHCP.Enabled {
		if _, e = m.Runner.Run(ctx, "dnsmasq", []string{"--test", "--conf-file=-"}, p.DHCPConfig); e != nil {
			add("error", "dhcp_syntax", "dnsmasq 不可用或候选配置校验失败")
		}
	}
	return r, nil
}
func (m *Manager) Check(ctx context.Context, role string, c Config) (CheckResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.check(ctx, role, c)
}
func equalSnapshot(a, b snapshot) bool {
	if a.NFT != b.NFT || a.DHCP != b.DHCP || a.DHCPActive != b.DHCPActive || a.DNSRouting != b.DNSRouting {
		return false
	}
	for k, v := range b.Sysctls {
		if a.Sysctls[k] != v {
			return false
		}
	}
	return true
}
func replaceNFT(current, target string) string {
	if current != "" {
		return "delete table inet " + TableName + "\n" + target
	}
	return target
}

func (m *Manager) Apply(ctx context.Context, role string, c Config) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !c.Enabled {
		return Status{}, fmt.Errorf("须显式启用网关后才能更改系统")
	}
	result, e := m.check(ctx, role, c)
	if e != nil {
		return Status{}, e
	}
	if !result.Ready {
		return Status{Findings: result.Findings}, fmt.Errorf("网关启用前检查未通过")
	}
	s, e := m.load()
	if e != nil {
		return Status{}, e
	}
	if s.Active != nil && s.Active.Plan.ConfigDigest == result.Plan.ConfigDigest {
		return m.status(ctx, s)
	}
	before, e := m.capture(ctx, result.Plan)
	if e != nil {
		return Status{}, e
	}
	// 更新计划时也保留旧计划涉及的键，用于安全恢复及 drift 检查。
	if s.Active != nil {
		for k := range s.Active.Current.Sysctls {
			if _, ok := before.Sysctls[k]; !ok {
				v, e := m.run(ctx, "sysctl", "-n", k)
				if e != nil {
					return Status{}, e
				}
				before.Sysctls[k] = strings.TrimSpace(v)
			}
		}
	}
	if s.Active != nil {
		for k := range before.Sysctls {
			if _, ok := result.Plan.Sysctls[k]; !ok {
				result.Plan.Sysctls[k] = s.Active.Baseline.Sysctls[k]
			}
		}
	}
	s.Pending = &transaction{Before: before, Target: result.Plan, ObservedDNSRouting: before.DNSRouting}
	if e = m.save(s); e != nil {
		return Status{}, e
	}
	applyErr := m.execute(ctx, &s)
	if applyErr != nil {
		// 请求取消后仍保留独立的有限恢复时间。
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		recoveryErr := m.restorePending(recoveryCtx, &s)
		if recoveryErr != nil {
			return Status{RecoveryRequired: true}, fmt.Errorf("网关应用失败且恢复需要处理: %v；%v", applyErr, recoveryErr)
		}
		return Status{}, fmt.Errorf("网关应用失败，已恢复应用前状态: %w", applyErr)
	}
	baseline := before
	if s.Active != nil {
		baseline = s.Active.Baseline
		for k, v := range before.Sysctls {
			if _, ok := baseline.Sysctls[k]; !ok {
				baseline.Sysctls[k] = v
			}
		}
	}
	current, e := m.capture(ctx, result.Plan)
	if e != nil {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if re := m.restorePending(recoveryCtx, &s); re != nil {
			return Status{RecoveryRequired: true}, fmt.Errorf("health check failed; recovery: %v", re)
		}
		return Status{}, fmt.Errorf("health check failed, restored prior state: %w", e)
	}
	for k := range before.Sysctls {
		if _, ok := current.Sysctls[k]; !ok {
			v, e := m.run(ctx, "sysctl", "-n", k)
			if e != nil {
				return Status{RecoveryRequired: true}, e
			}
			current.Sysctls[k] = strings.TrimSpace(v)
		}
	}
	expected := snapshot{DNSRouting: s.Pending.ObservedDNSRouting, NFT: s.Pending.ObservedNFT, Sysctls: result.Plan.Sysctls, DHCP: before.DHCP, DHCPActive: result.Plan.Config.DHCP.Enabled}
	if result.Plan.Config.DHCP.Enabled {
		expected.DHCP = result.Plan.DHCPConfig
	}
	if !equalSnapshot(current, expected) {
		return Status{RecoveryRequired: true, Drift: true}, fmt.Errorf("应用后的资源不符合计划，保留事务并拒绝接管外部更改")
	}
	previousActive, tx := s.Active, s.Pending
	s.Active = &activeState{Plan: result.Plan, Baseline: baseline, Current: current, AppliedAt: time.Now().UTC().Format(time.RFC3339)}
	s.Pending = nil
	if e = m.save(s); e != nil {
		s.Active = previousActive
		s.Pending = tx
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if restoreErr := m.restorePending(recoveryCtx, &s); restoreErr != nil {
			return Status{RecoveryRequired: true}, fmt.Errorf("应用记录保存失败；恢复需要处理: %v", restoreErr)
		}
		return Status{}, fmt.Errorf("应用记录保存失败，已恢复前一状态: %w", e)
	}
	return Status{Applied: true, ConfigDigest: s.Active.Plan.ConfigDigest, AppliedAt: s.Active.AppliedAt, Findings: []Finding{}}, nil
}
func (m *Manager) execute(ctx context.Context, s *state) error {
	tx := s.Pending
	p := tx.Target
	actual, e := m.nft(ctx)
	if e != nil {
		return e
	}
	if actual != tx.Before.NFT {
		return fmt.Errorf("nftables changed before apply")
	}
	if _, e := m.Runner.Run(ctx, "nft", []string{"-f", "-"}, replaceNFT(tx.Before.NFT, p.NFTables)); e != nil {
		return e
	}
	nft, e := m.nft(ctx)
	if e != nil {
		return e
	}
	tx.ObservedNFT = nft
	tx.NFTRecorded = true
	if e = m.save(*s); e != nil {
		return e
	}
	if e = m.applyDNSRouting(ctx, s); e != nil {
		return e
	}
	if e = m.writeSysctls(ctx, tx.Before.Sysctls, p.Sysctls); e != nil {
		return e
	}
	if p.Config.DHCP.Enabled {
		if e = atomicWrite(filepath.Join(m.StateDir, "dnsmasq.conf"), []byte(p.DHCPConfig)); e != nil {
			return e
		}
		if _, e = m.run(ctx, "systemctl", "restart", "sbm-gateway-dhcp.service"); e != nil {
			return e
		}
		out, e := m.run(ctx, "systemctl", "is-active", "sbm-gateway-dhcp.service")
		if e != nil || strings.TrimSpace(out) != "active" {
			return fmt.Errorf("DHCP 服务未健康启动")
		}
	} else if tx.Before.DHCPActive {
		if _, e = m.run(ctx, "systemctl", "stop", "sbm-gateway-dhcp.service"); e != nil {
			return e
		}
	}
	for k, v := range p.Sysctls {
		out, e := m.run(ctx, "sysctl", "-n", k)
		if e != nil || strings.TrimSpace(out) != v {
			return fmt.Errorf("系统参数健康检查失败: %s", k)
		}
	}
	return nil
}
func (m *Manager) restorePending(ctx context.Context, s *state) error {
	tx := s.Pending
	if tx == nil {
		return nil
	}
	live, e := m.capture(ctx, tx.Target)
	if e != nil {
		return e
	}
	if e = m.captureExtra(ctx, &live, tx.Before.Sysctls); e != nil {
		return e
	}
	if live.DNSRouting.Routes != tx.Before.DNSRouting.Routes && live.DNSRouting.Routes != tx.ObservedDNSRouting.Routes || live.DNSRouting.Rules != tx.Before.DNSRouting.Rules && live.DNSRouting.Rules != tx.ObservedDNSRouting.Rules {
		return fmt.Errorf("DNS 策略路由已漂移或未记录归属，拒绝恢复")
	}
	if live.NFT != tx.Before.NFT && (!tx.NFTRecorded || live.NFT != tx.ObservedNFT) {
		return fmt.Errorf("nftables 已变化或崩溃窗口未记录指纹，拒绝覆盖；需管理员核对所属表")
	}
	for k, v := range live.Sysctls {
		if v != tx.Before.Sysctls[k] && v != tx.Target.Sysctls[k] && !implicitForwardingValue(k, v, tx.Target.Sysctls) {
			return fmt.Errorf("sysctl %s 被外部修改，拒绝覆盖", k)
		}
	}
	if live.DHCP != tx.Before.DHCP && live.DHCP != tx.Target.DHCPConfig {
		return fmt.Errorf("DHCP 配置被外部修改，拒绝覆盖")
	}
	if e = m.restoreDNSRouting(ctx, tx, live.DNSRouting); e != nil {
		return e
	}
	if e = m.restore(ctx, live, tx.Before); e != nil {
		return e
	}
	if tx.RollingBack {
		s.Active = nil
	}
	s.Pending = nil
	return m.save(*s)
}
func (m *Manager) restore(ctx context.Context, live, target snapshot) error {
	if e := m.writeSysctls(ctx, live.Sysctls, target.Sysctls); e != nil {
		return e
	}
	if live.NFT != target.NFT {
		now, e := m.nft(ctx)
		if e != nil || now != live.NFT {
			return fmt.Errorf("nftables changed during restore")
		}
		if _, e := m.Runner.Run(ctx, "nft", []string{"-f", "-"}, replaceNFT(live.NFT, target.NFT)); e != nil {
			return e
		}
	}
	if live.DHCP != target.DHCP {
		if e := atomicWrite(filepath.Join(m.StateDir, "dnsmasq.conf"), []byte(target.DHCP)); e != nil {
			return e
		}
	}
	if target.DHCPActive {
		if _, e := m.run(ctx, "systemctl", "restart", "sbm-gateway-dhcp.service"); e != nil {
			return e
		}
	} else if live.DHCPActive {
		if _, e := m.run(ctx, "systemctl", "stop", "sbm-gateway-dhcp.service"); e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) Rollback(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.GOOS != "linux" {
		return Status{}, fmt.Errorf("恢复仅支持 Linux")
	}
	s, e := m.load()
	if e != nil {
		return Status{}, e
	}
	currentBoot, rebooted, finding := m.bootBoundary(s)
	if finding != nil {
		result := Status{Rebooted: rebooted, RecoveryRequired: true, Findings: []Finding{*finding}}
		if finding.Code == "boot_unavailable" {
			return result, fmt.Errorf("无法验证本次系统启动身份，拒绝恢复")
		}
		return m.recoverPreviousBoot(ctx, s, currentBoot, result)
	}
	if s.Pending != nil {
		if e = m.restorePending(ctx, &s); e != nil {
			return Status{RecoveryRequired: true}, e
		}
		return m.status(ctx, s)
	}
	if s.Active == nil {
		return Status{Findings: []Finding{}}, nil
	}
	live, e := m.capture(ctx, s.Active.Plan)
	if e != nil {
		return Status{}, e
	}
	for k := range s.Active.Current.Sysctls {
		if _, ok := live.Sysctls[k]; !ok {
			v, e := m.run(ctx, "sysctl", "-n", k)
			if e != nil {
				return Status{}, e
			}
			live.Sysctls[k] = strings.TrimSpace(v)
		}
	}
	if !equalSnapshot(live, s.Active.Current) {
		return Status{Drift: true}, fmt.Errorf("所属资源被外部修改，拒绝覆盖")
	}
	s.Pending = &transaction{RollingBack: true, Before: s.Active.Baseline, Target: s.Active.Plan, ObservedNFT: live.NFT, NFTRecorded: true, ObservedDNSRouting: live.DNSRouting}
	if e = m.save(s); e != nil {
		return Status{}, e
	}
	if e = m.restorePending(ctx, &s); e != nil {
		return Status{RecoveryRequired: true}, e
	}
	return Status{Findings: []Finding{}}, nil
}
func (m *Manager) Status(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, e := m.load()
	if e != nil {
		return Status{}, e
	}
	return m.status(ctx, s)
}
func (m *Manager) status(ctx context.Context, s state) (Status, error) {
	out := Status{RecoveryRequired: s.Pending != nil, Findings: []Finding{}}
	if m.GOOS == "linux" {
		value, _ := m.run(ctx, "systemctl", "is-active", "singbox-manager.service")
		value = strings.TrimSpace(value)
		if !includes([]string{"active", "inactive", "failed", "activating", "deactivating"}, value) {
			value = "unknown"
		}
		out.ManagerService = value
	}
	if s.Active != nil {
		out.ConfigDigest = s.Active.Plan.ConfigDigest
		out.AppliedAt = s.Active.AppliedAt
	}
	if m.GOOS == "linux" {
		_, rebooted, finding := m.bootBoundary(s)
		if finding != nil {
			out.Rebooted = rebooted
			out.RecoveryRequired = true
			out.Findings = append(out.Findings, *finding)
			return out, nil
		}
	}
	if s.Active == nil {
		return out, nil
	}
	out.Applied = true
	out.ConfigDigest = s.Active.Plan.ConfigDigest
	out.AppliedAt = s.Active.AppliedAt
	if m.GOOS != "linux" {
		return out, fmt.Errorf("状态检查仅支持 Linux")
	}
	current, e := m.capture(ctx, s.Active.Plan)
	if e != nil {
		return out, e
	}
	for k := range s.Active.Current.Sysctls {
		if _, ok := current.Sysctls[k]; !ok {
			v, e := m.run(ctx, "sysctl", "-n", k)
			if e != nil {
				return out, e
			}
			current.Sysctls[k] = strings.TrimSpace(v)
		}
	}
	out.Drift = !equalSnapshot(current, s.Active.Current)
	return out, nil
}

func (m *Manager) audit(ctx context.Context, c Config, applied bool, add func(string, string, string)) error {
	out, e := m.run(ctx, "ip", "-j", "address", "show", "dev", c.LANInterface)
	if e != nil {
		return fmt.Errorf("无法读取 LAN 接口")
	}
	var interfaces []interfaceAddresses
	if json.Unmarshal([]byte(out), &interfaces) != nil {
		return fmt.Errorf("接口检查输出无效")
	}
	found := false
	for _, i := range interfaces {
		for _, a := range i.AddrInfo {
			found = found || a.Local == c.LANAddress
		}
	}
	if !found {
		add("error", "lan_address", "所选 LAN 地址没有配置在 LAN 接口上")
	}
	if c.IPv6Mode == "proxy" && !hasConfiguredIPv6Prefixes(c, interfaces) {
		add("error", "lan_ipv6_prefix", "LAN 接口没有与全部配置 IPv6 前缀对应的已就绪单播地址")
	}
	if e = m.checkDefaultRoute(ctx, c, add); e != nil {
		return e
	}
	if _, e = m.run(ctx, "ip", "link", "show", "dev", c.UplinkInterface); e != nil {
		add("error", "uplink", "所选上游接口不存在")
	}
	out, e = m.run(ctx, "ip", "-j", "route", "get", c.UpstreamGateway)
	if e != nil {
		return e
	}
	var routes []struct {
		Dev string `json:"dev"`
	}
	if json.Unmarshal([]byte(out), &routes) != nil {
		return fmt.Errorf("路由检查输出无效")
	}
	if len(routes) == 0 || routes[0].Dev != c.UplinkInterface {
		add("error", "return_route", "上游网关的当前路由与所选上游接口不一致")
	}
	out, e = m.run(ctx, "ss", "-H", "-lntup")
	if e != nil {
		return e
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		port := fields[4]
		idx := strings.LastIndex(port, ":")
		if idx < 0 {
			continue
		}
		n, _ := strconv.Atoi(port[idx+1:])
		if n == 5354 || n == 9888 {
			add("error", "legacy_listener", "检测到旧 DNS/TProxy 监听（5354/9888），先停用旧实例再启用网关")
		}
		listenHost := strings.Trim(port[:idx], "[]")
		collision := listenHost == "*" || listenHost == "0.0.0.0" || listenHost == "::" || listenHost == c.LANAddress
		if n == c.DNSPort && collision && !m.ownedListener(line, false) {
			add("error", "dns_port", "LAN DNS 端口已被其他实例占用")
		}
		if c.AccessMode == "dns" && n == TProxyPort && !m.ownedListener(line, false) {
			add("error", "tproxy_port", "DNS 旁路透明监听端口已被其他实例占用")
		}
		if c.DHCP.Enabled && n == 67 && !m.ownedListener(line, true) {
			add("error", "dhcp_port", "DHCP 端口已被其他服务占用")
		}
	}
	out, e = m.run(ctx, "nft", "list", "ruleset")
	if e != nil {
		return e
	}
	foreignRules := withoutOwnedTable(out, applied)
	if c.AccessMode == "dns" && hasDNSMark(foreignRules) {
		add("error", "dns_mark", "既有 nftables 规则使用 DNS 旁路保留 mark，拒绝争用")
	}
	if strings.Contains(strings.ToLower(foreignRules), "tproxy") {
		add("error", "legacy_tproxy", "现有 nftables TProxy 规则与新接管冲突")
	}
	if (!applied || c.AccessMode == "dns") && hasSingboxTable(out) {
		add("error", "other_tun", "检测到既有 sing-box 自动重定向表，拒绝争抢实例资源")
	}
	if strings.Contains(strings.ToLower(out), "docker") {
		add("warning", "docker", "检测到 Docker 网络；核对排除网段和容器回程，不能依赖容器监听端口识别所有对端流量")
	}
	for _, legacy := range []struct{ tool, path string }{{"iptables-save", "/proc/net/ip_tables_names"}, {"ip6tables-save", "/proc/net/ip6_tables_names"}} {
		rules, e := m.run(ctx, legacy.tool)
		if e != nil {
			if names, readErr := os.ReadFile(legacy.path); readErr == nil && strings.TrimSpace(string(names)) != "" {
				add("error", "legacy_firewall_unchecked", "已加载 legacy iptables 表，但无法读取其规则；请安装对应检查工具")
			}
			continue
		}
		if c.AccessMode == "dns" && hasDNSMark(rules) {
			add("error", "dns_legacy_mark", "既有 iptables 规则使用 DNS 旁路保留 mark，拒绝争用")
		}
		if strings.Contains(strings.ToUpper(rules), "-J TPROXY") {
			add("error", "legacy_iptables_tproxy", "检测到 legacy iptables TProxy 接管，必须先停用旧接管")
		}
	}
	for _, family := range []string{"-4", "-6"} {
		out, e = m.run(ctx, "ip", "-N", family, "rule", "show")
		if e != nil {
			return e
		}
		for _, line := range strings.Split(out, "\n") {
			if hasPair(line, "lookup", "100") || hasPair(line, "fwmark", "0x1") {
				add("error", "legacy_policy_route", "检测到旧透明代理策略路由（表 100/mark 1），须先人工确认和移除旧接管")
			}
			if !applied && (hasPair(line, "lookup", "2022") || strings.HasPrefix(strings.TrimSpace(line), "9000:")) {
				add("error", "tun_policy_route", "TUN 默认表 2022/规则 9000 已被占用")
			}
			if strings.TrimSpace(line) != "" && !(c.AccessMode == "dns" && applied && validDNSRule(canonicalLines(line), c)) && !supportedPolicyRule(line, applied && c.AccessMode != "dns") {
				add("error", "complex_policy_route", "检测到无法确认归属的策略路由；请先核对或隔离该规则，管理器不会自动改写路由")
			}

		}
	}
	if c.DHCP.Enabled {
		add("warning", "dhcp_broadcast", "本机检查不能发现其他设备上的 DHCP；启用前必须确认广播域中无冲突服务器")
	}
	return nil
}
func (m *Manager) ownedListener(line string, dhcp bool) bool {
	segments := strings.Split(line, "pid=")
	if len(segments) < 2 {
		return false
	}
	for _, segment := range segments[1:] {
		end := strings.IndexFunc(segment, func(r rune) bool { return r < '0' || r > '9' })
		if end >= 0 {
			segment = segment[:end]
		}
		if !m.ownedPID(segment, dhcp) {
			return false
		}
	}
	return true
}
func (m *Manager) ownedPID(pid string, dhcp bool) bool {
	n, e := strconv.Atoi(pid)
	if e != nil || n < 1 {
		return false
	}
	executable, e := os.Readlink(filepath.Join("/proc", pid, "exe"))
	if e != nil {
		return false
	}
	args, e := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if e != nil {
		return false
	}
	parts := strings.Split(strings.TrimRight(string(args), "\x00"), "\x00")
	if dhcp {
		if executable != trustedExecutable("dnsmasq") {
			return false
		}
		cgroup, e := os.ReadFile(filepath.Join("/proc", pid, "cgroup"))
		if e != nil {
			return false
		}
		owned := false
		for _, line := range strings.Split(string(cgroup), "\n") {
			if strings.HasSuffix(line, "/sbm-gateway-dhcp.service") {
				owned = true
			}
		}
		return owned && includes(parts, "--conf-file="+filepath.Join(m.StateDir, "dnsmasq.conf"))
	}
	cwd, e := os.Readlink(filepath.Join("/proc", pid, "cwd"))
	if e != nil {
		return false
	}
	return managedIdentity(m.Policy, executable, cwd, parts)
}
func managedIdentity(p Policy, executable, cwd string, args []string) bool {
	dataDir := p.ManagedDataDir
	if dataDir == "" {
		dataDir = filepath.Dir(filepath.Dir(p.ManagedConfig))
	}
	return executable == p.ManagedExecutable && cwd == dataDir && len(args) == 4 && args[1] == "run" && args[2] == "-c" && args[3] == p.ManagedConfig
}

func (m *Manager) captureExtra(ctx context.Context, s *snapshot, keys map[string]string) error {
	for k := range keys {
		if _, ok := s.Sysctls[k]; !ok {
			v, e := m.run(ctx, "sysctl", "-n", k)
			if e != nil {
				return e
			}
			s.Sysctls[k] = strings.TrimSpace(v)
		}
	}
	return nil
}

func hasPair(line, first, second string) bool {
	v := strings.Fields(line)
	for i := 0; i+1 < len(v); i++ {
		if v[i] == first && v[i+1] == second {
			return true
		}
	}
	return false
}

// RestartManager 仅重启固定系统服务；异步提交让调用者有机会接收响应。
func (m *Manager) RestartManager(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.GOOS != "linux" {
		return Status{}, fmt.Errorf("系统服务管理仅支持 Linux")
	}
	s, e := m.load()
	if e != nil {
		return Status{}, e
	}
	if s.Active == nil || s.Pending != nil {
		return Status{}, fmt.Errorf("须有已应用且无待恢复事务的网关实例")
	}
	if e = m.Policy.authorize("gateway", s.Active.Plan.Config); e != nil {
		return Status{}, e
	}
	result, e := m.status(ctx, s)
	if e != nil {
		return result, e
	}
	if result.RecoveryRequired || result.Rebooted || result.Drift || !result.Applied {
		return result, fmt.Errorf("网关状态需要先恢复，拒绝重启管理服务")
	}
	if _, e = m.run(ctx, "systemctl", "--no-block", "restart", "singbox-manager.service"); e != nil {
		return result, e
	}
	result.ManagerService = "restarting"
	return result, nil
}

// 全局 forwarding 有内核联动：把受影响接口及默认值纳入快照，保留无关接口原值。
func (m *Manager) completeForwardingPlan(ctx context.Context, p *Plan, active *activeState) error {
	if p.Config.AccessMode == "dns" {
		return nil
	}
	raw, e := m.run(ctx, "ip", "-j", "link", "show")
	if e != nil {
		return e
	}
	var links []struct {
		Name string `json:"ifname"`
	}
	if json.Unmarshal([]byte(raw), &links) != nil {
		return fmt.Errorf("接口清单格式无效")
	}
	families := []string{"ipv4"}
	if p.Config.IPv6Mode == "proxy" {
		families = append(families, "ipv6")
	}
	p.Sysctls["net.ipv4.conf.all.accept_redirects"] = "0"
	for _, family := range families {
		p.Sysctls["net."+family+".conf.default.forwarding"] = "1"
		for _, link := range links {
			if !interfacePattern.MatchString(link.Name) {
				return fmt.Errorf("接口名称无法安全表达为系统参数")
			}
			key := "net/" + family + "/conf/" + link.Name + "/forwarding"
			if link.Name == p.Config.LANInterface || link.Name == p.Config.UplinkInterface || link.Name == TUNInterface {
				p.Sysctls[key] = "1"
				continue
			}
			if active != nil {
				if v, ok := active.Baseline.Sysctls[key]; ok {
					p.Sysctls[key] = v
					continue
				}
			}
			value, e := m.run(ctx, "sysctl", "-n", key)
			if e != nil {
				return e
			}
			p.Sysctls[key] = strings.TrimSpace(value)
		}
	}
	return nil
}
func implicitForwardingValue(key, value string, globals map[string]string) bool {
	family := "ipv4"
	if strings.Contains(key, "ipv6") {
		family = "ipv6"
	}
	globalKey := "net.ipv4.ip_forward"
	if family == "ipv6" {
		globalKey = "net.ipv6.conf.all.forwarding"
	}
	v, ok := globals[globalKey]
	if !ok {
		return false
	}
	if key == "net.ipv4.conf.all.accept_redirects" {
		expected := "0"
		if v == "0" {
			expected = "1"
		}
		return value == expected
	}
	affected := key == "net."+family+".conf.default.forwarding" || strings.HasPrefix(key, "net/"+family+"/conf/") && strings.HasSuffix(key, "/forwarding")
	return affected && value == v
}
func (m *Manager) writeSysctls(ctx context.Context, from, to map[string]string) error {
	keys := sortedKeys(to)
	ordered := []string{}
	globals := map[string]string{}
	for _, g := range []string{"net.ipv4.ip_forward", "net.ipv6.conf.all.forwarding"} {
		if _, ok := to[g]; ok {
			ordered = append(ordered, g)
		}
	}
	for _, k := range keys {
		if k != "net.ipv4.ip_forward" && k != "net.ipv6.conf.all.forwarding" {
			ordered = append(ordered, k)
		}
	}
	for _, k := range ordered {
		raw, e := m.run(ctx, "sysctl", "-n", k)
		if e != nil {
			return e
		}
		actual := strings.TrimSpace(raw)
		if actual != from[k] && actual != to[k] && !implicitForwardingValue(k, actual, globals) {
			return fmt.Errorf("sysctl %s 被外部修改，拒绝覆盖", k)
		}
		if actual != to[k] {
			if _, e = m.run(ctx, "sysctl", "-w", k+"="+to[k]); e != nil {
				return e
			}
			if k == "net.ipv4.ip_forward" || k == "net.ipv6.conf.all.forwarding" {
				globals[k] = to[k]
			}
		}
	}
	return nil
}

func hasSingboxTable(rules string) bool {
	for _, line := range strings.Split(rules, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "table" && strings.Trim(f[2], "\"") == "sing-box" {
			return true
		}
	}
	return false
}
