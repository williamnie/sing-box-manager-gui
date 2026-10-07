package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	addresses     string
	defaultRoutes string
	ipRules       string
	dnsRoutes     string
	dnsRules      string
	sysctls       map[string]string
	nft           string
	rules         string
	listeners     string
	mutations     int
	dhcpActive    bool
	fail          func(string, []string, string) bool
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, in string) (string, error) {
	mutation := name == "ip" && len(args) > 2 && (args[2] == "add" || args[2] == "del") || name == "sysctl" && args[0] == "-w" || name == "nft" && args[0] == "-f" || name == "systemctl" && (args[0] == "restart" || args[0] == "stop")
	if mutation {
		r.mutations++
	}
	if r.fail != nil && r.fail(name, args, in) {
		return "", errors.New("injected failure")
	}
	switch name {
	case "sysctl":
		if args[0] == "-n" {
			v, ok := r.sysctls[args[1]]
			if !ok {
				v = "0"
			}
			return v, nil
		}
		p := strings.SplitN(args[1], "=", 2)
		r.sysctls[p[0]] = p[1]
		if p[0] == "net.ipv4.ip_forward" {
			for _, iface := range []string{"lo", "eth0", "docker0"} {
				r.sysctls["net/ipv4/conf/"+iface+"/forwarding"] = p[1]
			}
			r.sysctls["net.ipv4.conf.default.forwarding"] = p[1]
			v := "0"
			if p[1] == "0" {
				v = "1"
			}
			r.sysctls["net.ipv4.conf.all.accept_redirects"] = v
		}
		if p[0] == "net.ipv6.conf.all.forwarding" {
			for _, iface := range []string{"lo", "eth0", "docker0"} {
				r.sysctls["net/ipv6/conf/"+iface+"/forwarding"] = p[1]
			}
		}
		return "", nil
	case "nft":
		if args[0] == "--check" {
			return "", nil
		}
		if args[0] == "-f" {
			r.nft = strings.TrimPrefix(in, "delete table inet "+TableName+"\n")
			return "", nil
		}
		if strings.Join(args, " ") == "-s list tables" {
			if r.nft != "" {
				return "table inet " + TableName + "\n", nil
			}
			return "", nil
		}
		if strings.Join(args, " ") == "list ruleset" {
			return r.rules, nil
		}
		return r.nft, nil
	case "systemctl":
		if args[0] == "is-active" {
			if r.dhcpActive {
				return "active", nil
			}
			return "inactive", errors.New("inactive")
		}
		r.dhcpActive = args[0] == "restart"
		return "", nil
	case "ss":
		return r.listeners, nil
	case "ip":
		if len(args) > 0 && args[0] == "-N" {
			args = args[1:]
		}
		if strings.Join(args, " ") == "-4 route show table 20230" {
			return r.dnsRoutes, nil
		}
		if len(args) > 2 && args[0] == "-4" && (args[2] == "add" || args[2] == "del") {
			if args[1] == "route" {
				if args[2] == "add" {
					r.dnsRoutes = "2 " + args[4] + " dev lo proto 242 scope 254"
				} else {
					r.dnsRoutes = ""
				}
			}
			if args[1] == "rule" {
				if args[2] == "add" {
					values := map[string]string{}
					for i := 3; i+1 < len(args); i += 2 {
						values[args[i]] = args[i+1]
					}
					r.dnsRules = "12030: from all"
					if values["to"] != "" {
						r.dnsRules += " to " + values["to"]
					}
					r.dnsRules += " fwmark 0x5342 iif " + values["iif"] + " lookup 20230 proto 242"
				} else {
					r.dnsRules = ""
				}
			}
			return "", nil
		}
		if strings.Join(args, " ") == "-j link show" {
			return `[{"ifname":"lo"},{"ifname":"eth0"},{"ifname":"docker0"}]`, nil
		}
		if len(args) > 2 && args[0] == "-j" && args[1] == "address" {
			if r.addresses != "" {
				return r.addresses, nil
			}
			return `[{"addr_info":[{"local":"192.0.2.142","prefixlen":24},{"local":"fd00::142","prefixlen":64,"family":"inet6","scope":"global"}]}]`, nil
		}
		if strings.Join(args, " ") == "-j -4 route show table main default" {
			if r.defaultRoutes != "" {
				return r.defaultRoutes, nil
			}
			return `[{"dst":"default","gateway":"192.0.2.1","dev":"eth0","metric":100}]`, nil
		}
		if args[0] == "-j" && args[1] == "route" {
			return `[{"dev":"eth0"}]`, nil
		}
		if args[0] == "-4" || args[0] == "-6" {
			if r.ipRules != "" {
				return r.ipRules + "\n" + r.dnsRules, nil
			}
			return "0: from all lookup local\n32766: from all lookup main\n" + r.dnsRules, nil
		}
		return "", nil
	case "iptables-save", "ip6tables-save":
		return "", nil
	case "dnsmasq":
		return "syntax check OK", nil
	}
	return "", fmt.Errorf("unexpected command %s %v", name, args)
}
func fixture(t *testing.T) (*Manager, *fakeRunner, Config) {
	t.Helper()
	r := &fakeRunner{sysctls: map[string]string{"net.ipv4.ip_forward": "0", "net.ipv4.conf.all.send_redirects": "1", "net/ipv4/conf/eth0/send_redirects": "1", "net.ipv4.conf.all.rp_filter": "1", "net/ipv4/conf/eth0/rp_filter": "1", "net.ipv6.conf.all.forwarding": "0", "net/ipv6/conf/eth0/accept_ra": "1"}}
	c := Config{Enabled: true, LANInterface: "eth0", LANCIDRs: []string{"192.0.2.0/24"}, LANAddress: "192.0.2.142", UpstreamGateway: "192.0.2.1", IPv6Mode: "disabled"}
	m := NewManager(r, Policy{AllowGateway: true, AllowedLANInterfaces: []string{"eth0"}, AllowedUplinkInterfaces: []string{"eth0"}, AllowedLANCIDRs: []string{"192.0.2.0/24", "fd00::/64"}, AllowDHCP: true, AllowNAT: true}, t.TempDir())
	m.GOOS = "linux"
	m.ReadBootID = func() (string, error) { return "test-boot-one", nil }
	return m, r, c
}
func TestApplyOptInAndPlatform(t *testing.T) {
	for _, tc := range []string{"disabled", "desktop", "darwin", "policy", "interface", "cidr", "nat", "dhcp"} {
		t.Run(tc, func(t *testing.T) {
			m, r, c := fixture(t)
			role := "gateway"
			switch tc {
			case "disabled":
				c.Enabled = false
			case "desktop":
				role = "desktop"
			case "darwin":
				m.GOOS = "darwin"
			case "policy":
				m.Policy.AllowGateway = false
			case "interface":
				m.Policy.AllowedLANInterfaces = nil
			case "cidr":
				m.Policy.AllowedLANCIDRs = nil
			case "nat":
				c.NAT = true
				m.Policy.AllowNAT = false
			case "dhcp":
				c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.200", RangeEnd: "192.0.2.210"}
				m.Policy.AllowDHCP = false
			}
			if _, e := m.Apply(context.Background(), role, c); e == nil {
				t.Fatal("expected rejection")
			}
			if r.mutations != 0 {
				t.Fatalf("unauthorized mutations: %d", r.mutations)
			}
		})
	}
}
func TestDraftCheckAndPreviewDoNotMutate(t *testing.T) {
	m, r, c := fixture(t)
	c.Enabled = false
	p, e := Preview("gateway", c)
	if e != nil || p.Config.Enabled {
		t.Fatal(e)
	}
	result, e := m.Check(context.Background(), "gateway", c)
	if e != nil || !result.Ready {
		t.Fatalf("draft check: %v %+v", e, result.Findings)
	}
	if r.mutations != 0 {
		t.Fatal("preview/check mutated")
	}
}
func TestApplyIdempotentRollback(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	status, e := m.Apply(ctx, "gateway", c)
	if e != nil || !status.Applied || status.Drift {
		t.Fatalf("apply: %+v %v", status, e)
	}
	n := r.mutations
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if r.mutations != n {
		t.Fatal("identical application was not idempotent")
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.nft != "" || r.sysctls["net.ipv4.ip_forward"] != "0" || r.sysctls["net/ipv4/conf/eth0/rp_filter"] != "1" {
		t.Fatalf("baseline not restored: %+v", r)
	}
	n = r.mutations
	if _, e = m.Rollback(ctx); e != nil || r.mutations != n {
		t.Fatal("rollback not idempotent", e)
	}
}
func TestApplyFailureRestoresPreviousState(t *testing.T) {
	m, r, c := fixture(t)
	failed := false
	r.fail = func(name string, args []string, _ string) bool {
		if !failed && name == "nft" && args[0] == "-f" {
			failed = true
			return true
		}
		return false
	}
	if _, e := m.Apply(context.Background(), "gateway", c); e == nil {
		t.Fatal("expected failure")
	}
	if r.nft != "" || r.sysctls["net.ipv4.ip_forward"] != "0" || r.sysctls["net.ipv4.conf.all.send_redirects"] != "1" {
		t.Fatal("failed application did not restore sysctls")
	}
	st, e := m.Status(context.Background())
	if e != nil || st.Applied || st.RecoveryRequired {
		t.Fatalf("pending recovery after successful restore: %+v %v", st, e)
	}
}
func TestFailedUpdateKeepsActiveConfig(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	first, e := m.Apply(ctx, "gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	oldNFT := r.nft
	c.NAT = true
	failed := false
	r.fail = func(n string, a []string, _ string) bool {
		if !failed && n == "nft" && a[0] == "-f" {
			failed = true
			return true
		}
		return false
	}
	if _, e = m.Apply(ctx, "gateway", c); e == nil {
		t.Fatal("expected failure")
	}
	status, e := m.Status(ctx)
	if e != nil || status.ConfigDigest != first.ConfigDigest || status.Drift || r.nft != oldNFT {
		t.Fatalf("old application lost: %+v %v", status, e)
	}
}
func TestExternalDriftNeverOverwritten(t *testing.T) {
	for _, target := range []string{"table", "sysctl", "dhcp"} {
		t.Run(target, func(t *testing.T) {
			m, r, c := fixture(t)
			ctx := context.Background()
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			switch target {
			case "table":
				r.nft += "# external\n"
			case "sysctl":
				r.sysctls["net.ipv4.ip_forward"] = "0"
			case "dhcp":
				if e := os.WriteFile(filepath.Join(m.StateDir, "dnsmasq.conf"), []byte("external"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			n := r.mutations
			if _, e := m.Apply(ctx, "gateway", c); e == nil {
				t.Fatal("expected drift rejection")
			}
			if _, e := m.Rollback(ctx); e == nil {
				t.Fatal("expected rollback drift rejection")
			}
			if r.mutations != n {
				t.Fatal("external state overwritten")
			}
		})
	}
}
func TestForeignTableNeverAdopted(t *testing.T) {
	m, r, c := fixture(t)
	r.nft = "table inet sbm_gateway {}\n"
	if _, e := m.Apply(context.Background(), "gateway", c); e == nil || r.mutations != 0 {
		t.Fatal("foreign table adopted")
	}
}
func TestLegacyConflictDetection(t *testing.T) {
	for _, which := range []string{"tproxy", "dns", "legacy_dns", "legacy_tproxy", "other_tun"} {
		t.Run(which, func(t *testing.T) {
			m, r, c := fixture(t)
			switch which {
			case "tproxy":
				r.rules = "tproxy to :9888"
			case "dns":
				r.listeners = "udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:*"
			case "legacy_dns":
				r.listeners = "udp UNCONN 0 0 *:5354 *:*"
			case "legacy_tproxy":
				r.listeners = "tcp LISTEN 0 0 *:9888 *:*"
			case "other_tun":
				r.rules = "table inet sing-box {}"
			}
			if _, e := m.Apply(context.Background(), "gateway", c); e == nil || r.mutations != 0 {
				t.Fatal("conflict allowed")
			}
		})
	}
}
func TestIPv6TransitionPreservesBaseline(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	c.IPv6Mode = "proxy"
	c.LANCIDRs = append(c.LANCIDRs, "fd00::/64")
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if r.sysctls["net.ipv6.conf.all.forwarding"] != "1" {
		t.Fatal("ipv6 disabled")
	}
	c.IPv6Mode = "disabled"
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if r.sysctls["net.ipv6.conf.all.forwarding"] != "0" || r.sysctls["net/ipv6/conf/eth0/accept_ra"] != "1" {
		t.Fatal("obsolete sysctls not restored")
	}
	if _, e := m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestDHCPFailureRestoresNftAndService(t *testing.T) {
	m, r, c := fixture(t)
	c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.200", RangeEnd: "192.0.2.220"}
	r.fail = func(n string, a []string, _ string) bool { return n == "systemctl" && a[0] == "restart" }
	if _, e := m.Apply(context.Background(), "gateway", c); e == nil {
		t.Fatal("expected DHCP failure")
	}
	if r.nft != "" || r.dhcpActive || r.sysctls["net.ipv4.ip_forward"] != "0" {
		t.Fatal("DHCP failure did not rollback")
	}
	b, e := os.ReadFile(filepath.Join(m.StateDir, "dnsmasq.conf"))
	if e != nil || len(b) != 0 {
		t.Fatal("DHCP config not restored")
	}
}
func TestRecoveryAfterInterruptedRollback(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	failed := false
	r.fail = func(n string, a []string, _ string) bool {
		if !failed && n == "sysctl" && a[0] == "-w" {
			failed = true
			return true
		}
		return false
	}
	if _, e := m.Rollback(ctx); e == nil {
		t.Fatal("expected interruption")
	}
	r.fail = nil
	restart := NewManager(r, m.Policy, m.StateDir)
	restart.GOOS = "linux"
	restart.ReadBootID = m.ReadBootID
	if _, e := restart.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.nft != "" || r.sysctls["net.ipv4.ip_forward"] != "0" {
		t.Fatal("interrupted rollback not recovered")
	}
}
func TestUnrecordedNftCrashWindowFailsClosed(t *testing.T) {
	m, r, c := fixture(t)
	p, _ := Preview("gateway", c)
	before, e := m.capture(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.save(state{Pending: &transaction{Before: before, Target: p}}); e != nil {
		t.Fatal(e)
	}
	r.nft = p.NFTables
	if _, e = m.Rollback(context.Background()); e == nil {
		t.Fatal("unverified crash-window table overwritten")
	}
	if r.mutations != 0 {
		t.Fatal("unverified table touched")
	}
}
func TestRPCRejectsUnknownFieldsAndLargeBody(t *testing.T) {
	m, r, _ := fixture(t)
	for _, body := range []string{`{"command":"rm"}`, `{} {}`, strings.Repeat(" ", 65<<10) + `{}`} {
		req := httptest.NewRequest("POST", "/apply", strings.NewReader(body))
		w := httptest.NewRecorder()
		Handler(m).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid request accepted: %d", w.Code)
		}
	}
	if r.mutations != 0 {
		t.Fatal("invalid request mutated")
	}
}

func TestLocalResolverDoesNotConflictWithLANListener(t *testing.T) {
	m, r, c := fixture(t)
	r.listeners = "udp UNCONN 0 0 127.0.0.53:53 0.0.0.0:*"
	v, e := m.Check(context.Background(), "gateway", c)
	if e != nil || !v.Ready {
		t.Fatalf("separate bind address rejected: %+v %v", v, e)
	}
}
func TestExternalChangeAfterNftApplyNotAdopted(t *testing.T) {
	m, r, c := fixture(t)
	reads := 0
	r.fail = func(name string, args []string, _ string) bool {
		if name == "nft" && strings.Join(args, " ") == "-s list table inet sbm_gateway" {
			reads++
			if reads == 2 {
				r.nft += "# changed externally\n"
			}
		}
		return false
	}
	v, e := m.Apply(context.Background(), "gateway", c)
	if e == nil || !v.RecoveryRequired {
		t.Fatalf("post-apply drift not detected: %+v %v", v, e)
	}
	s, e := m.load()
	if e != nil || s.Active != nil || s.Pending == nil {
		t.Fatal("external state adopted")
	}
}
func TestSysctlFailureRestoresProtectionAndParameters(t *testing.T) {
	m, r, c := fixture(t)
	writes := 0
	r.fail = func(name string, args []string, _ string) bool {
		if name == "sysctl" && args[0] == "-w" {
			writes++
			return writes == 3
		}
		return false
	}
	if _, e := m.Apply(context.Background(), "gateway", c); e == nil {
		t.Fatal("expected failure")
	}
	if r.nft != "" || r.sysctls["net.ipv4.conf.all.rp_filter"] != "1" || r.sysctls["net.ipv4.conf.all.send_redirects"] != "1" {
		t.Fatal("partial sysctl apply not restored")
	}
}
func TestRestartManagerRequiresOwnedActiveGateway(t *testing.T) {
	m, _, c := fixture(t)
	ctx := context.Background()
	if _, e := m.RestartManager(ctx); e == nil {
		t.Fatal("restart without active gateway allowed")
	}
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	v, e := m.RestartManager(ctx)
	if e != nil || v.ManagerService != "restarting" {
		t.Fatal(v, e)
	}
}

func TestManagedIdentityRequiresExactInvocation(t *testing.T) {
	p := Policy{ManagedExecutable: "/data/bin/sing-box", ManagedConfig: "/data/generated/config.json", ManagedDataDir: "/data"}
	args := []string{"/data/bin/sing-box", "run", "-c", "/data/generated/config.json"}
	if !managedIdentity(p, p.ManagedExecutable, "/data", args) {
		t.Fatal("expected managed identity")
	}
	for _, v := range [][]string{append(append([]string{}, args...), "-c", "/other.json"), {"sing-box", "check", "-c", p.ManagedConfig}, {"sing-box", "run", "-c", "/other.json"}} {
		if managedIdentity(p, p.ManagedExecutable, "/data", v) {
			t.Fatal("wrong invocation accepted", v)
		}
	}
	if managedIdentity(p, p.ManagedExecutable, "/other", args) || managedIdentity(p, "/other/bin/sing-box", "/data", args) {
		t.Fatal("foreign executable/cwd accepted")
	}
}

func TestKernelForwardingSideEffectsPreserveDockerAndRedirectBaseline(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	r.sysctls["net/ipv4/conf/docker0/forwarding"] = "1"
	r.sysctls["net.ipv4.conf.all.accept_redirects"] = "0"
	r.sysctls["net.ipv4.conf.default.forwarding"] = "0"
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if r.sysctls["net/ipv4/conf/docker0/forwarding"] != "1" || r.sysctls["net/ipv4/conf/lo/forwarding"] != "0" || r.sysctls["net/ipv4/conf/eth0/forwarding"] != "1" {
		t.Fatal("unrelated interface forwarding changed")
	}
	if _, e := m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	for k, want := range map[string]string{"net.ipv4.ip_forward": "0", "net/ipv4/conf/docker0/forwarding": "1", "net/ipv4/conf/eth0/forwarding": "0", "net.ipv4.conf.all.accept_redirects": "0", "net.ipv4.conf.default.forwarding": "0"} {
		if r.sysctls[k] != want {
			t.Errorf("%s=%s, want %s", k, r.sysctls[k], want)
		}
	}
}
