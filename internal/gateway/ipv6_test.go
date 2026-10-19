package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func autoIPv6Fixture(t *testing.T) (*Manager, *fakeRunner, Config) {
	t.Helper()
	m, r, c := fixture(t)
	c.IPv6Mode, c.IPv6LANSource = "proxy", "auto"
	m.Policy.DynamicIPv6Interfaces = []string{"eth0"}
	setIPv6Network(r, "2001:db8:1::")
	return m, r, c
}

func setIPv6Network(r *fakeRunner, prefix string) {
	r.addresses = fmt.Sprintf(`[{"ifname":"eth0","addr_info":[{"local":"192.0.2.142","prefixlen":24},{"local":"%s142","prefixlen":64,"scope":"global","valid_life_time":600}]}]`, prefix)
	r.ipv6Routes = fmt.Sprintf(`[{"dst":"%s/64","dev":"eth0","protocol":"ra","expires":600}]`, prefix)
}

func TestAutoIPv6RequiresIndependentAuthorization(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	m.Policy.DynamicIPv6Interfaces = nil
	if v, e := m.Check(context.Background(), "gateway", c); e != nil || v.Ready || !findingCode(v.Findings, "permission") || r.mutations != 0 {
		t.Fatalf("missing root authorization was accepted: %+v %v", v, e)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.IPv6Mode = "disabled" },
		func(c *Config) { c.AccessMode = "dns" },
		func(c *Config) { c.LANCIDRs = append(c.LANCIDRs, "2001:db8:1::/64") },
		func(c *Config) { c.BypassCIDRs = []string{"2001:db8:1::2/128"} },
	} {
		copy := c
		mutate(&copy)
		if Validate(copy) == nil {
			t.Fatalf("unsafe automatic configuration accepted: %+v", copy)
		}
	}
}

func TestAutoIPv6ExclusionsCannotExpandRootAuthorization(t *testing.T) {
	m, _, c := autoIPv6Fixture(t)
	c = Normalize(c)
	c.ExcludeCIDRs = []string{"2001:db8:abcd::/64"}
	if e := m.Policy.authorize("gateway", c); e == nil {
		t.Fatal("Web created an unauthorized IPv6 direct exception")
	}
	m.Policy.DynamicIPv6Exclusions = []string{"2001:db8:abcd::/64"}
	if e := m.Policy.authorize("gateway", c); e != nil {
		t.Fatal(e)
	}
	c.ExcludeCIDRs = []string{"2001:db8::/32"}
	if e := m.Policy.authorize("gateway", c); e == nil {
		t.Fatal("narrow authorization allowed wider exception")
	}
}

func TestAutoIPv6DiscoveryUsesAddressesAndOnLinkRoutes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*Manager, *fakeRunner)
		want      []string
		wantError bool
	}{
		{"ready", func(*Manager, *fakeRunner) {}, []string{"2001:db8:1::/64"}, false},
		{"filtered_iproute_omits_dev", func(_ *Manager, r *fakeRunner) { r.ipv6Routes = strings.Replace(r.ipv6Routes, `"dev":"eth0",`, "", 1) }, []string{"2001:db8:1::/64"}, false},
		{"temporary_deprecated_still_valid", func(_ *Manager, r *fakeRunner) {
			r.addresses = strings.Replace(r.addresses, `"scope":"global"`, `"scope":"global","deprecated":true,"flags":["temporary"],"preferred_life_time":0`, 1)
		}, []string{"2001:db8:1::/64"}, false},
		{"dhcp_address_128", func(_ *Manager, r *fakeRunner) {
			r.addresses = strings.Replace(r.addresses, `"prefixlen":64`, `"prefixlen":128`, 1)
		}, []string{"2001:db8:1::/64"}, false},
		{"tentative", func(_ *Manager, r *fakeRunner) {
			r.addresses = strings.Replace(r.addresses, `"scope":"global"`, `"scope":"global","tentative":true`, 1)
		}, []string{}, false},
		{"dadfailed", func(_ *Manager, r *fakeRunner) {
			r.addresses = strings.Replace(r.addresses, `"scope":"global"`, `"scope":"global","flags":["dadfailed"]`, 1)
		}, []string{}, false},
		{"expired_address", func(_ *Manager, r *fakeRunner) {
			r.addresses = strings.Replace(r.addresses, `"valid_life_time":600`, `"valid_life_time":0`, 1)
		}, []string{}, false},
		{"expired_route", func(_ *Manager, r *fakeRunner) {
			r.ipv6Routes = strings.Replace(r.ipv6Routes, `"expires":600`, `"expires":0`, 1)
		}, []string{}, false},
		{"via_router", func(_ *Manager, r *fakeRunner) {
			r.ipv6Routes = strings.Replace(r.ipv6Routes, `"protocol":"ra"`, `"protocol":"ra","gateway":"fe80::1"`, 1)
		}, []string{}, false},
		{"wrong_interface", func(_ *Manager, r *fakeRunner) {
			r.ipv6Routes = strings.Replace(r.ipv6Routes, `"dev":"eth0"`, `"dev":"eth1"`, 1)
		}, []string{}, false},
		{"ula_requires_opt_in", func(_ *Manager, r *fakeRunner) { setIPv6Network(r, "fd00::") }, []string{}, false},
		{"ula_authorized", func(m *Manager, r *fakeRunner) { setIPv6Network(r, "fd00::"); m.Policy.DynamicIPv6AllowULA = true }, []string{"fd00::/64"}, false},
		{"outside_root_scope", func(m *Manager, _ *fakeRunner) { m.Policy.DynamicIPv6Within = []string{"2001:db8:2::/64"} }, []string{}, false},
		{"invalid_root_scope", func(m *Manager, _ *fakeRunner) { m.Policy.DynamicIPv6Within = []string{"::/0"} }, nil, true},
		{"default_route", func(_ *Manager, r *fakeRunner) {
			r.ipv6Routes = strings.Replace(r.ipv6Routes, `2001:db8:1::/64`, `::/0`, 1)
		}, []string{}, false},
		{"delegated_aggregate", func(_ *Manager, r *fakeRunner) {
			r.ipv6Routes = strings.Replace(r.ipv6Routes, `2001:db8:1::/64`, `2001:db8:1::/48`, 1)
		}, []string{}, false},
		{"local_host_route", func(_ *Manager, r *fakeRunner) {
			r.ipv6Routes = strings.Replace(r.ipv6Routes, `2001:db8:1::/64`, `2001:db8:1::142/128`, 1)
		}, []string{}, false},
		{"malformed", func(_ *Manager, r *fakeRunner) { r.ipv6Routes = "{" }, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, r, c := autoIPv6Fixture(t)
			tc.change(m, r)
			got, e := m.discoverIPv6(context.Background(), c)
			if (e != nil) != tc.wantError || !reflect.DeepEqual(got, tc.want) || r.mutations != 0 {
				t.Fatalf("got %v, %v; want %v, error=%v", got, e, tc.want, tc.wantError)
			}
		})
	}
}

func TestAutoIPv6RenumberingKeepsGuardDigestAndOtherResources(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	before, _ := m.load()
	setIPv6Network(r, "2001:db8:2::")
	r.fail = func(name string, args []string, input string) bool {
		if name == "sysctl" && args[0] == "-w" || name == "systemctl" && args[0] != "is-active" || name == "nft" && args[0] == "-f" && !strings.HasPrefix(input, "flush set ") {
			t.Fatalf("prefix refresh changed other resources: %s %v %s", name, args, input)
		}
		return false
	}
	if e := m.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	status, e := m.Status(ctx)
	if e != nil || status.Drift || status.RecoveryRequired || status.IPv6.Generation != 2 || !reflect.DeepEqual(status.IPv6.Prefixes, []string{"2001:db8:2::/64"}) {
		t.Fatalf("bad runtime state: %+v %v", status, e)
	}
	if status.ConfigDigest != before.Active.Plan.ConfigDigest || status.AppliedAt != before.Active.AppliedAt {
		t.Fatal("renumbering changed user configuration identity")
	}
	assertAutoIPv6Guard(t, r.nft)
	n := r.mutations
	if e := m.SyncIPv6(ctx); e != nil || r.mutations != n {
		t.Fatal("unchanged prefixes mutated system", e)
	}
	r.fail = nil
	r.ipv6Routes = "[]"
	if e := m.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	assertAutoIPv6Guard(t, r.nft)
	if strings.Contains(r.nft, "2001:db8:") {
		t.Fatal("removed prefix still exempted")
	}
	n = r.mutations
	if e := m.SyncIPv6(ctx); e != nil || r.mutations != n {
		t.Fatal("empty set is not idempotent", e)
	}
	if _, e := m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.nft != "" {
		t.Fatal("rollback left auto resources")
	}
}

func assertAutoIPv6Guard(t *testing.T, nft string) {
	t.Helper()
	if strings.Contains(nft, "ip6 saddr") || !strings.Contains(nft, `iifname "eth0" meta nfproto ipv6 oifname != "sbm-tun" counter reject`) || !strings.Contains(nft, "icmpv6 type nd-redirect counter drop") || strings.Contains(nft, "{  }") {
		t.Fatalf("missing unconditional protection: %s", nft)
	}
}

func TestAutoIPv6DiscoveryFailureClearsExceptionsButNotGuard(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	r.ipv6Routes = "malformed"
	if e := m.SyncIPv6(ctx); e == nil {
		t.Fatal("discovery failure hidden")
	}
	s, e := m.Status(ctx)
	if e != nil || s.Drift || len(s.IPv6.Prefixes) != 0 || s.IPv6.Error == "" {
		t.Fatalf("bad degraded state: %+v %v", s, e)
	}
	assertAutoIPv6Guard(t, r.nft)
}

func TestAutoIPv6DriftAndPreviousBootNeverOverwritten(t *testing.T) {
	for _, reboot := range []bool{false, true} {
		t.Run(fmt.Sprint(reboot), func(t *testing.T) {
			m, r, c := autoIPv6Fixture(t)
			ctx := context.Background()
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			if reboot {
				m.ReadBootID = func() (string, error) { return "different-boot", nil }
			} else {
				r.nft += "# external change"
			}
			setIPv6Network(r, "2001:db8:2::")
			n := r.mutations
			if e := m.SyncIPv6(ctx); e == nil || r.mutations != n {
				t.Fatal("unsafe auto write", e)
			}
		})
	}
}

func TestAutoIPv6FailedAtomicUpdatePreservesRecordedState(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.200", RangeEnd: "192.0.2.220"}
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	before := r.nft
	setIPv6Network(r, "2001:db8:2::")
	r.fail = func(name string, args []string, in string) bool {
		if name == "systemctl" && args[0] != "is-active" || name == "sysctl" && args[0] == "-w" {
			t.Fatalf("failed prefix update touched DHCP/sysctl: %s %v", name, args)
		}
		return name == "nft" && args[0] == "-f" && strings.HasPrefix(in, "flush set ")
	}
	if e := m.SyncIPv6(ctx); e == nil {
		t.Fatal("failure hidden")
	}
	if r.nft != before {
		t.Fatal("failed transaction changed protection")
	}
	s, _ := m.load()
	if s.Pending != nil || s.Active.IPv6Generation != 1 {
		t.Fatal("failed update changed active generation")
	}
}

func TestManualIPv6DigestRemainsCompatible(t *testing.T) {
	_, _, c := fixture(t)
	p, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	c.IPv6LANSource = "manual"
	manual, e := Preview("gateway", c)
	if e != nil || manual.ConfigDigest != p.ConfigDigest || manual.NFTables != p.NFTables {
		t.Fatal("manual mode changed legacy identity", e)
	}
	b, _ := json.Marshal(manual.Config)
	if strings.Contains(string(b), "ipv6_lan_source") {
		t.Fatal("manual source must not change old serialized configurations")
	}
}

func TestAutoIPv6CrashWindowDoesNotGuessResourceOwnership(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	setIPv6Network(r, "2001:db8:2::")
	committed, failedRead := false, false
	r.fail = func(name string, args []string, in string) bool {
		if name == "nft" && args[0] == "-f" && strings.HasPrefix(in, "flush set ") {
			committed = true
		}
		if committed && !failedRead && name == "nft" && strings.Join(args, " ") == "-s list tables" {
			failedRead = true
			return true
		}
		return false
	}
	if e := m.SyncIPv6(ctx); e == nil {
		t.Fatal("unrecorded commit was hidden")
	}
	state, e := m.load()
	if e != nil || state.Pending == nil || state.Pending.NFTRecorded {
		t.Fatalf("crash window record: %+v %v", state, e)
	}
	assertAutoIPv6Guard(t, r.nft)
	r.fail = nil
	n := r.mutations
	if _, e := m.Rollback(ctx); e == nil || r.mutations != n {
		t.Fatal("unrecorded table was overwritten", e)
	}
}

func TestAutoIPv6InterruptedRecordedUpdateRestoresOnlySet(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.200", RangeEnd: "192.0.2.220"}
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	s, e := m.load()
	if e != nil {
		t.Fatal(e)
	}
	p, _ := Preview("gateway", s.Active.Plan.Config)
	p = withIPv6Prefixes(p, []string{"2001:db8:2::/64"})
	p.Sysctls = s.Active.Plan.Sysctls
	s.Pending = &transaction{IPv6Only: true, Before: s.Active.Current, Target: p}
	if e := m.save(s); e != nil {
		t.Fatal(e)
	}
	if e := m.execute(ctx, &s); e != nil {
		t.Fatal(e)
	}
	// 模拟集合更新并记录指纹后进程退出；新 Manager 只能恢复这次集合变更。
	restarted := NewManager(r, m.Policy, m.StateDir)
	restarted.GOOS = m.GOOS
	restarted.ReadBootID = m.ReadBootID
	r.fail = func(name string, args []string, input string) bool {
		if name == "sysctl" && args[0] == "-w" || name == "systemctl" && args[0] != "is-active" || name == "nft" && args[0] == "-f" && !strings.HasPrefix(input, "flush set ") {
			t.Fatalf("set recovery touched other resources: %s %v", name, args)
		}
		return false
	}
	if _, e := restarted.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.nft != s.Active.Current.NFT {
		t.Fatal("original set not restored")
	}
	status, e := restarted.Status(ctx)
	if e != nil || status.Drift || status.RecoveryRequired || status.IPv6.Generation != 1 {
		t.Fatalf("bad restored state: %+v %v", status, e)
	}
}

func TestAutoIPv6TemplateUpgradeRequiresExplicitApply(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	s, _ := m.load()
	s.Active.Plan.NFTables = strings.Replace(s.Active.Plan.NFTables, "priority -10", "priority -9", 1)
	s.Active.Current.NFT = s.Active.Plan.NFTables
	r.nft = s.Active.Current.NFT
	if e := m.save(s); e != nil {
		t.Fatal(e)
	}
	n := r.mutations
	if e := m.SyncIPv6(ctx); e == nil || r.mutations != n {
		t.Fatal("template upgrade silently changed rules", e)
	}
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.nft, "priority -9") {
		t.Fatal("explicit apply incorrectly updated only elements")
	}
}

func TestAutoIPv6UnchangedPrefixesSkipFullCapture(t *testing.T) {
	m, r, c := autoIPv6Fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if e := m.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	captures := 0
	r.fail = func(name string, args []string, _ string) bool {
		if name == "sysctl" && args[0] == "-n" {
			captures++
		}
		return false
	}
	// 前缀未变且刚完成完整校验：只做前缀发现，不重复采集整套资源。
	if e := m.SyncIPv6(ctx); e != nil || captures != 0 {
		t.Fatalf("unchanged prefixes captured system state: %d %v", captures, e)
	}
	// 校验过期后恢复完整采集，持续检测外部修改。
	m.ipv6VerifiedAt = time.Now().Add(-ipv6VerifyInterval)
	if e := m.SyncIPv6(ctx); e != nil || captures == 0 {
		t.Fatalf("expired verification skipped capture: %d %v", captures, e)
	}
	// 前缀变化时必须先完整校验再写入。
	captures = 0
	r.nft += "# external change"
	setIPv6Network(r, "2001:db8:2::")
	n := r.mutations
	if e := m.SyncIPv6(ctx); e == nil || captures == 0 || r.mutations != n {
		t.Fatal("changed prefixes bypassed drift check", e)
	}
}
