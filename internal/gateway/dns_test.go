package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func dnsFixture(t *testing.T) (*Manager, *fakeRunner, Config) {
	m, r, c := fixture(t)
	c.AccessMode = "dns"
	c.FakeIPRange = "198.18.0.0/15"
	c.StaticRouteConfirmed = true
	c.LANAddress = "192.0.2.2"
	c.UpstreamGateway = "192.0.2.1"
	c.LANCIDRs = []string{"192.0.2.0/24"}
	m.Policy.AllowedLANCIDRs = c.LANCIDRs
	m.Policy.AllowDNSBypass = true
	r.addresses = `[{"addr_info":[{"local":"192.0.2.2","prefixlen":24}]}]`
	r.defaultRoutes = `[{"dst":"default","gateway":"192.0.2.1","dev":"eth0"}]`
	return m, r, c
}

func TestDNSPreviewScopeAndPrerequisites(t *testing.T) {
	_, _, c := dnsFixture(t)
	c.StaticRouteConfirmed = false
	c.BypassCIDRs = []string{"192.0.2.9/32"}
	p, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	for _, required := range []string{`iifname "eth0" ip saddr { 192.0.2.0/24 } ip daddr 198.18.0.0/15 meta l4proto { tcp, udp }`, `tproxy ip to :9898`, `meta mark set 0x5342`} {
		if !strings.Contains(p.NFTables, required) {
			t.Fatalf("missing scope %s", required)
		}
	}
	for _, forbidden := range []string{"hook output", "hook forward", "masquerade", "192.0.2.9/32 return", "flush"} {
		if strings.Contains(p.NFTables, forbidden) {
			t.Fatalf("unexpected interception %s", forbidden)
		}
	}
	if len(p.Sysctls) != 1 || p.Sysctls["net/ipv4/conf/eth0/rp_filter"] != "2" || p.DHCPConfig != "" {
		t.Fatalf("excess resources: %+v", p)
	}
	if len(p.PolicyRoutes) != 2 || !strings.Contains(p.PolicyRoutes[1], "to 198.18.0.0/15 iif eth0 fwmark 0x5342/0xffffffff") {
		t.Fatalf("unscoped route: %+v", p.PolicyRoutes)
	}
}

func TestDNSRejectUnsafeConfigAndMissingOptIn(t *testing.T) {
	for _, which := range []string{"confirmation", "policy", "nat", "dhcp", "ipv6", "wide_pool", "outside_pool", "pool_overlap", "source", "dns_port"} {
		t.Run(which, func(t *testing.T) {
			m, r, c := dnsFixture(t)
			switch which {
			case "confirmation":
				c.StaticRouteConfirmed = false
			case "policy":
				m.Policy.AllowDNSBypass = false
			case "nat":
				c.NAT = true
			case "dhcp":
				c.DHCP.Enabled = true
			case "ipv6":
				c.IPv6Mode = "proxy"
			case "wide_pool":
				c.FakeIPRange = "198.18.0.0/14"
			case "outside_pool":
				c.FakeIPRange = "203.0.113.0/24"
			case "pool_overlap":
				c.ExcludeCIDRs = []string{c.FakeIPRange}
			case "source":
				c.DNSSource = "unknown"
			case "dns_port":
				c.DNSPort = 5353
			}
			if _, e := m.Apply(context.Background(), "gateway", c); e == nil {
				t.Fatal("unsafe mode accepted")
			}
			if r.mutations != 0 {
				t.Fatal("rejected configuration mutated system")
			}
		})
	}
}

func TestDNSApplyIdempotentRollbackPreservesOtherResources(t *testing.T) {
	m, r, c := dnsFixture(t)
	ctx := context.Background()
	r.sysctls["net.ipv4.ip_forward"] = "1" // 主机已有 Docker/转发策略。
	baseline := map[string]string{}
	for k, v := range r.sysctls {
		baseline[k] = v
	}
	result, e := m.Apply(ctx, "gateway", c)
	if e != nil || !result.Applied {
		t.Fatalf("apply: %+v %v", result, e)
	}
	if !validDNSRoute(r.dnsRoutes, c) || !validDNSRule(r.dnsRules, c) {
		t.Fatalf("invalid routing: %s; %s", r.dnsRoutes, r.dnsRules)
	}
	for k, v := range baseline {
		if k != "net/ipv4/conf/eth0/rp_filter" && r.sysctls[k] != v {
			t.Fatalf("changed unrelated %s", k)
		}
	}
	before := r.mutations
	// 真实 nft list ruleset 包含自己的 TProxy，重复应用不能自报冲突。
	r.rules = r.nft + "\ntable ip docker {\n chain forward { }\n}\n"
	if _, e = m.Apply(ctx, "gateway", c); e != nil || r.mutations != before {
		t.Fatalf("not idempotent: %v", e)
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.dnsRoutes != "" || r.dnsRules != "" || r.nft != "" {
		t.Fatal("owned resources remained")
	}
	for k, v := range baseline {
		if r.sysctls[k] != v {
			t.Fatalf("baseline %s not restored", k)
		}
	}
	before = r.mutations
	if _, e = m.Rollback(ctx); e != nil || r.mutations != before {
		t.Fatal("rollback not idempotent", e)
	}
}

func TestDNSConflictsNeverOverwritten(t *testing.T) {
	for _, which := range []string{"route", "priority", "mark", "table", "port", "old_tproxy", "other_tun", "nft_mark"} {
		t.Run(which, func(t *testing.T) {
			m, r, c := dnsFixture(t)
			switch which {
			case "route":
				r.dnsRoutes = "blackhole 198.18.0.0/15"
			case "priority":
				r.ipRules = "12030: from all lookup main"
			case "mark":
				r.ipRules = "15000: from all fwmark 0x5342 lookup 555"
			case "table":
				r.ipRules = "15000: from all lookup 20230"
			case "port":
				r.listeners = "tcp LISTEN 0 0 127.0.0.1:9898 0.0.0.0:*"
			case "old_tproxy":
				r.rules = "table ip legacy { tproxy to :9888 }"
			case "nft_mark":
				r.rules = "table ip docker { meta mark set 0x00005342 }"
			case "other_tun":
				r.rules = "table inet sing-box {}"
			}
			if _, e := m.Apply(context.Background(), "gateway", c); e == nil || r.mutations != 0 {
				t.Fatal("foreign resource adopted")
			}
		})
	}
}

func TestDNSFailureRestoresEveryRecordedStep(t *testing.T) {
	for _, which := range []string{"nft", "route", "rule", "sysctl"} {
		t.Run(which, func(t *testing.T) {
			m, r, c := dnsFixture(t)
			failed := false
			r.fail = func(name string, args []string, _ string) bool {
				match := which == "nft" && name == "nft" && args[0] == "-f" || which == "route" && name == "ip" && len(args) > 2 && args[1] == "route" && args[2] == "add" || which == "rule" && name == "ip" && len(args) > 2 && args[1] == "rule" && args[2] == "add" || which == "sysctl" && name == "sysctl" && args[0] == "-w"
				if match && !failed {
					failed = true
					return true
				}
				return false
			}
			if _, e := m.Apply(context.Background(), "gateway", c); e == nil {
				t.Fatal("expected failure")
			}
			if !failed || r.dnsRoutes != "" || r.dnsRules != "" || r.nft != "" || r.sysctls["net/ipv4/conf/eth0/rp_filter"] != "1" {
				t.Fatalf("partial failure not restored: %+v", r)
			}
			s, e := m.Status(context.Background())
			if e != nil || s.RecoveryRequired || s.Applied {
				t.Fatal(s, e)
			}
		})
	}
}

func TestDNSDriftStopsApplyRollbackAndRestart(t *testing.T) {
	for _, which := range []string{"route", "rule"} {
		t.Run(which, func(t *testing.T) {
			m, r, c := dnsFixture(t)
			ctx := context.Background()
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			if which == "route" {
				r.dnsRoutes += "\nblackhole 198.19.0.0/24"
			} else {
				r.dnsRules += "\n12030: from all lookup main"
			}
			before := r.mutations
			s, e := m.Status(ctx)
			if e != nil || !s.Drift {
				t.Fatal(s, e)
			}
			for _, run := range []func() error{func() error { _, e := m.Apply(ctx, "gateway", c); return e }, func() error { _, e := m.Rollback(ctx); return e }, func() error { _, e := m.RestartManager(ctx); return e }} {
				if run() == nil {
					t.Fatal("drift accepted")
				}
			}
			if r.mutations != before {
				t.Fatal("foreign route mutated")
			}
		})
	}
}

func TestDNSCrashWindowWithoutRecordedFingerprintFailsClosed(t *testing.T) {
	m, r, c := dnsFixture(t)
	ctx := context.Background()
	p, _ := Preview("gateway", c)
	before, e := m.capture(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	// 写路由成功但观察指纹还没 fsync：重启不得推测归属。
	if _, e = r.Run(ctx, "ip", dnsRouteArgs("add", c), ""); e != nil {
		t.Fatal(e)
	}
	if e = m.save(state{Pending: &transaction{Before: before, Target: p}}); e != nil {
		t.Fatal(e)
	}
	mutations := r.mutations
	if _, e = m.Rollback(ctx); e == nil {
		t.Fatal("unrecorded route deleted")
	}
	if mutations != r.mutations || r.dnsRoutes == "" {
		t.Fatal("unknown ownership overwritten")
	}
}

func TestDNSInterruptedRollbackCanResumeWithoutGlobalDelete(t *testing.T) {
	m, r, c := dnsFixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	failed := false
	r.fail = func(name string, args []string, _ string) bool {
		if name == "ip" && len(args) > 2 && args[1] == "route" && args[2] == "del" && !failed {
			failed = true
			return true
		}
		return false
	}
	if _, e := m.Rollback(ctx); e == nil {
		t.Fatal("expected interruption")
	}
	if r.dnsRules != "" || r.dnsRoutes == "" {
		t.Fatal("expected only rule removed")
	}
	r.fail = nil
	restart := NewManager(r, m.Policy, m.StateDir)
	restart.GOOS = "linux"
	restart.ReadBootID = m.ReadBootID
	if _, e := restart.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.dnsRoutes != "" || r.dnsRules != "" || r.nft != "" {
		t.Fatal("interrupted resources remained")
	}
}

func TestDNSModeAndPoolChangesRequireRollback(t *testing.T) {
	for _, which := range []string{"to_full", "to_dns", "pool", "interface"} {
		t.Run(which, func(t *testing.T) {
			m, r, c := dnsFixture(t)
			ctx := context.Background()
			if which == "to_dns" {
				c.AccessMode = "full"
			}
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			switch which {
			case "to_full":
				c.AccessMode = "full"
			case "to_dns":
				c.AccessMode = "dns"
			case "pool":
				c.FakeIPRange = "198.18.0.0/16"
			case "interface":
				c.LANInterface = "eth1"
				m.Policy.AllowedLANInterfaces = append(m.Policy.AllowedLANInterfaces, "eth1")
			}
			before := r.mutations
			if _, e := m.Apply(ctx, "gateway", c); e == nil || r.mutations != before {
				t.Fatal("live ownership switched")
			}
			if _, e := m.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestDNSPreviousBootCannotDeleteNewRoutes(t *testing.T) {
	for _, remains := range []bool{true, false} {
		t.Run(fmt.Sprint(remains), func(t *testing.T) {
			m, r, c := dnsFixture(t)
			ctx := context.Background()
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			m.ReadBootID = func() (string, error) { return "new-boot", nil }
			r.nft = ""
			if !remains {
				r.dnsRoutes = ""
				r.dnsRules = ""
			}
			before := r.mutations
			result, e := m.Rollback(ctx)
			if remains && (e == nil || !result.RecoveryRequired) || !remains && (e != nil || result.RecoveryRequired) {
				t.Fatal(result, e)
			}
			if before != r.mutations {
				t.Fatal("new boot resources mutated")
			}
		})
	}
}

func TestDNSRuleOwnershipRequiresAllConstraints(t *testing.T) {
	_, _, c := dnsFixture(t)
	valid := "12030: from all to 198.18.0.0/15 fwmark 0x5342 iif eth0 lookup 20230 proto 242"
	if !validDNSRule(valid, c) {
		t.Fatal("expected owned rule")
	}
	for _, value := range []string{strings.Replace(valid, "to 198.18.0.0/15 ", "", 1), strings.Replace(valid, "iif eth0 ", "", 1), strings.Replace(valid, "0x5342", "0x5342/0xffff", 1), valid + " suppress_prefixlength 0", valid + "\n" + valid} {
		if validDNSRule(value, c) {
			t.Fatal("unscoped rule accepted", value)
		}
	}
}

func TestLegacyFullConfigDigestAndActiveStateRemainCompatible(t *testing.T) {
	m, r, c := dnsFixture(t)
	c.AccessMode = "full"
	// 升级前 Normalize + json.Marshal(Config) 的确切输出，不包含 DNS 接入字段。
	oldJSON := `{"bypass_cidrs":null,"enabled":true,"lan_interface":"eth0","lan_cidrs":["192.0.2.0/24"],"lan_address":"192.0.2.2","upstream_gateway":"192.0.2.1","uplink_interface":"eth0","ipv6_mode":"disabled","nat":false,"exclude_cidrs":null,"dns_port":53,"dhcp":{"enabled":false,"range_start":"","range_end":"","lease_time":"12h","reservations":null}}`
	plan, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	if plan.ConfigDigest != digest(oldJSON) {
		t.Fatalf("old full digest changed: got %s, want %s", plan.ConfigDigest, digest(oldJSON))
	}
	ctx := context.Background()
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	old, e := m.load()
	if e != nil {
		t.Fatal(e)
	}
	old.Active.Plan.Config.AccessMode = ""
	old.Active.Plan.Config.FakeIPRange = ""
	old.Active.Plan.Config.DNSSource = ""
	old.Active.Plan.Config.StaticRouteConfirmed = false
	if e = m.save(old); e != nil {
		t.Fatal(e)
	}
	mutations := r.mutations
	if _, e = m.Apply(ctx, "gateway", c); e != nil || r.mutations != mutations {
		t.Fatal("old active state reapplied or rejected", e)
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
}
