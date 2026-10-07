package gateway

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// 此测试必须显式启用，并在新建网络 namespace 内运行。不会调用宿主 systemd。
func TestLinuxNamespace(t *testing.T) {
	if os.Getenv("SBM_GATEWAY_NETNS_TEST") != "1" {
		t.Skip("set SBM_GATEWAY_NETNS_TEST=1 on an isolated Linux test machine")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("requires Linux root with network namespace capability")
	}
	parentNS := os.Getenv("SBM_GATEWAY_PARENT_NETNS")
	currentNS, e := os.Readlink("/proc/self/ns/net")
	if e != nil {
		t.Fatal(e)
	}
	if parentNS == "" {
		unshare := trustedExecutable("unshare")
		if unshare == "" {
			t.Fatal("unshare is required")
		}
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(unshare, "--net", "--", exe, "-test.run=^TestLinuxNamespace$", "-test.v")
		cmd.Env = append(os.Environ(), "SBM_GATEWAY_PARENT_NETNS="+currentNS)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("isolated child: %v\n%s", e, out)
		}
		t.Log(string(out))
		return
	}
	if currentNS == parentNS {
		t.Fatal("refusing to modify the parent network namespace")
	}
	runner, e := NewSystemRunner()
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	for _, args := range [][]string{{"link", "set", "lo", "up"}, {"link", "add", "eth0", "type", "dummy"}, {"address", "add", "192.0.2.142/24", "dev", "eth0"}, {"link", "set", "eth0", "up"}, {"route", "add", "default", "via", "192.0.2.1", "dev", "eth0"}} {
		if _, e = runner.Run(ctx, "ip", args, ""); e != nil {
			t.Fatal(e)
		}
	}
	c := Config{Enabled: true, LANInterface: "eth0", LANAddress: "192.0.2.142", LANCIDRs: []string{"192.0.2.0/24"}, UpstreamGateway: "192.0.2.1", BypassCIDRs: []string{"192.0.2.205/32"}}
	m := NewManager(namespaceRunner{runner}, Policy{AllowGateway: true, AllowDNSBypass: true, AllowedLANInterfaces: []string{"eth0"}, AllowedUplinkInterfaces: []string{"eth0"}, AllowedLANCIDRs: c.LANCIDRs}, t.TempDir())
	baseline, e := runner.Run(ctx, "sysctl", []string{"-n", "net.ipv4.ip_forward"}, "")
	if e != nil {
		t.Fatal(e)
	}
	if status, e := m.Apply(ctx, "gateway", c); e != nil || !status.Applied || status.Drift {
		t.Fatalf("apply: %+v %v", status, e)
	}
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatalf("repeat apply: %v", e)
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	after, e := runner.Run(ctx, "sysctl", []string{"-n", "net.ipv4.ip_forward"}, "")
	if e != nil || strings.TrimSpace(after) != strings.TrimSpace(baseline) {
		t.Fatal("forwarding baseline not restored", e)
	}
	if table, e := m.nft(ctx); e != nil || table != "" {
		t.Fatal("owned nftables table not removed", e)
	}

	// 同一隔离 namespace 验证第二种接入的真实 nft 语法及路由匹配。
	c.AccessMode = "dns"
	c.StaticRouteConfirmed = true
	if status, e := m.Apply(ctx, "gateway", c); e != nil || !status.Applied {
		t.Fatalf("DNS apply: %+v %v", status, e)
	}
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatalf("DNS repeat apply: %v", e)
	}
	dns, e := m.captureDNSRouting(ctx)
	if e != nil || !validDNSRoute(dns.Routes, Normalize(c)) || !validDNSRule(dns.Rules, Normalize(c)) {
		t.Fatalf("DNS route ownership: %+v %v", dns, e)
	}
	for _, tc := range []struct {
		destination, mark string
		local             bool
	}{
		{"198.18.0.10", DNSMark, true},
		{"198.18.0.10", "0", false},
		{"203.0.113.10", DNSMark, false},
	} {
		out, e := runner.Run(ctx, "ip", []string{"-4", "route", "get", tc.destination, "from", "192.0.2.205", "iif", "eth0", "mark", tc.mark}, "")
		if tc.local && (e != nil || !strings.Contains(out, "local ")) || !tc.local && strings.Contains(out, "local ") {
			t.Fatalf("scoped route lookup %+v: %s %v", tc, out, e)
		}
	}
	after, e = runner.Run(ctx, "sysctl", []string{"-n", "net.ipv4.ip_forward"}, "")
	if e != nil || strings.TrimSpace(after) != strings.TrimSpace(baseline) {
		t.Fatal("DNS mode changed global forwarding", e)
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if dns, e = m.captureDNSRouting(ctx); e != nil || dns != (dnsRouting{}) {
		t.Fatalf("DNS routes remained: %+v %v", dns, e)
	}

	// 主路由可转交真实公网 IP；只允许对应入口和完整 mark 本地投递。
	c.CaptureRoutedTraffic = true
	m.Policy.AllowRoutedTraffic = true
	if status, e := m.Apply(ctx, "gateway", c); e != nil || !status.Applied {
		t.Fatalf("routed DNS ingress apply: %+v %v", status, e)
	}
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		mark, incoming string
		local          bool
	}{{DNSMark, "eth0", true}, {"0", "eth0", false}, {DNSMark, "lo", false}} {
		out, err := runner.Run(ctx, "ip", []string{"-4", "route", "get", "203.0.113.10", "from", "192.0.2.205", "iif", tc.incoming, "mark", tc.mark}, "")
		if tc.local && (err != nil || !strings.Contains(out, "local ")) || !tc.local && strings.Contains(out, "local ") {
			t.Fatalf("routed ingress lookup %+v: %s %v", tc, out, err)
		}
	}
	after, e = runner.Run(ctx, "sysctl", []string{"-n", "net.ipv4.ip_forward"}, "")
	if e != nil || strings.TrimSpace(after) != strings.TrimSpace(baseline) {
		t.Fatal("routed DNS ingress changed forwarding", e)
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if dns, e = m.captureDNSRouting(ctx); e != nil || dns != (dnsRouting{}) {
		t.Fatalf("routed DNS ingress was not restored: %+v %v", dns, e)
	}
}

type namespaceRunner struct{ Runner }

func (r namespaceRunner) Run(ctx context.Context, name string, args []string, input string) (string, error) {
	if name == "systemctl" {
		if len(args) > 0 && args[0] == "is-active" {
			return "inactive", fmt.Errorf("isolated service stub")
		}
		return "", fmt.Errorf("host systemd operations are forbidden in namespace tests")
	}
	return r.Runner.Run(ctx, name, args, input)
}
