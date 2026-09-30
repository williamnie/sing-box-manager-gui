package gateway

import (
	"context"
	"testing"
)

func TestActualMainDefaultRouteMustMatchConfiguredGateway(t *testing.T) {
	cases := []struct {
		name, routes string
		ready        bool
		code         string
	}{
		{"missing", `[]`, false, "default_route_missing"},
		{"different_gateway", `[{"dst":"default","dev":"eth0","gateway":"192.0.2.254"}]`, false, "default_route_mismatch"},
		{"different_interface", `[{"dst":"default","dev":"eth1","gateway":"192.0.2.1"}]`, false, "default_route_mismatch"},
		{"prefer_matching_lowest_metric", `[{"dst":"default","dev":"eth1","gateway":"192.0.2.254","metric":200},{"dst":"default","dev":"eth0","gateway":"192.0.2.1","metric":10}]`, true, ""},
		{"matching_backup_is_insufficient", `[{"dst":"default","dev":"eth1","gateway":"192.0.2.254","metric":10},{"dst":"default","dev":"eth0","gateway":"192.0.2.1","metric":200}]`, false, "default_route_mismatch"},
		{"skip_linkdown", `[{"dst":"default","dev":"eth1","gateway":"192.0.2.254","metric":1,"flags":["linkdown"]},{"dst":"default","dev":"eth0","gateway":"192.0.2.1","metric":200}]`, true, ""},
		{"equal_cost_other_path", `[{"dst":"default","dev":"eth0","gateway":"192.0.2.1","metric":10},{"dst":"default","dev":"eth1","gateway":"192.0.2.254","metric":10}]`, false, "default_route_mismatch"},
		{"multipath", `[{"dst":"default","nexthops":[{"gateway":"192.0.2.1","dev":"eth0"}]}]`, false, "default_route_complex"},
		{"nhid", `[{"dst":"default","nhid":20}]`, false, "default_route_complex"},
		{"blackhole", `[{"dst":"default","type":"blackhole"},{"dst":"default","dev":"eth0","gateway":"192.0.2.1","metric":200}]`, false, "default_route_complex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, r, c := fixture(t)
			r.defaultRoutes = tc.routes
			v, e := m.Check(context.Background(), "gateway", c)
			if e != nil || v.Ready != tc.ready {
				t.Fatalf("check: %+v %v", v, e)
			}
			if !tc.ready && !findingCode(v.Findings, tc.code) {
				t.Fatalf("missing specific finding: %+v", v.Findings)
			}
			if r.mutations != 0 {
				t.Fatal("route check modified system")
			}
		})
	}
}

func TestComplexPolicyRoutingRequiresManualResolution(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	r.ipRules = "0: from all lookup local\n1000: from 192.0.2.0/24 lookup 200\n32766: from all lookup main"
	v, e := m.Check(ctx, "gateway", c)
	if e != nil || v.Ready || !findingCode(v.Findings, "complex_policy_route") {
		t.Fatalf("custom routing accepted: %+v %v", v, e)
	}
	if _, e = m.Apply(ctx, "gateway", c); e == nil || r.mutations != 0 {
		t.Fatal("custom route silently changed or accepted")
	}
	r.ipRules = ""
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	r.ipRules = "0: from all lookup local\n9000: from all lookup main suppress_prefixlength 0\n9001: not from all fwmark 0x2024 lookup 2022\n32766: from all lookup main\n32768: from all lookup main"
	v, e = m.Check(ctx, "gateway", c)
	if e != nil || !v.Ready {
		t.Fatalf("own automatic rule range rejected: %+v %v", v, e)
	}
}

func TestIPv6ProxyNeedsReadyLANPrefix(t *testing.T) {
	cases := []struct {
		name, addresses string
		ready           bool
	}{
		{"missing", `[{"addr_info":[{"local":"192.0.2.142","prefixlen":24}]}]`, false},
		{"link_local_only", `[{"addr_info":[{"local":"192.0.2.142"},{"local":"fe80::142","prefixlen":64}]}]`, false},
		{"other_prefix", `[{"addr_info":[{"local":"192.0.2.142"},{"local":"fd01::142","prefixlen":64}]}]`, false},
		{"ready", `[{"addr_info":[{"local":"192.0.2.142"},{"local":"fd00::142","prefixlen":64}]}]`, true},
		{"tentative", `[{"addr_info":[{"local":"192.0.2.142"},{"local":"fd00::142","prefixlen":64,"tentative":true}]}]`, false},
		{"dad_failed", `[{"addr_info":[{"local":"192.0.2.142"},{"local":"fd00::142","prefixlen":64,"dadfailed":true}]}]`, false},
		{"expired_preferred", `[{"addr_info":[{"local":"192.0.2.142"},{"local":"fd00::142","prefixlen":64,"preferred_life_time":0}]}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, r, c := fixture(t)
			c.IPv6Mode = "proxy"
			c.LANCIDRs = append(c.LANCIDRs, "fd00::/64")
			r.addresses = tc.addresses
			v, e := m.Check(context.Background(), "gateway", c)
			if e != nil || v.Ready != tc.ready {
				t.Fatalf("IPv6 check: %+v %v", v, e)
			}
			if !tc.ready && !findingCode(v.Findings, "lan_ipv6_prefix") {
				t.Fatal("missing IPv6 prefix finding")
			}
			if r.mutations != 0 {
				t.Fatal("prefix check changed system")
			}
		})
	}
}
