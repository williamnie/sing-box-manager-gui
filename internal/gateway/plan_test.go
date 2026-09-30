package gateway

import (
	"strings"
	"testing"
)

func TestPlanFailClosedAndExplicitBypass(t *testing.T) {
	_, _, c := fixture(t)
	c.BypassCIDRs = []string{"192.0.2.205/32"}
	p, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	for _, required := range []string{`iifname "eth0" ip saddr 192.0.2.205/32 return`, `oifname != "sbm-tun"`, `counter reject with icmp type admin-prohibited`, `meta l4proto { tcp, udp }`, `meta nfproto ipv6`} {
		if !strings.Contains(p.NFTables, required) {
			t.Fatalf("missing %s", required)
		}
	}
	if strings.Contains(p.NFTables, "masquerade") {
		t.Fatal("NAT enabled by default")
	}
	if strings.Index(p.NFTables, "192.0.2.205/32 return") > strings.Index(p.NFTables, `oifname != "sbm-tun"`) {
		t.Fatal("explicit bypass after rejection")
	}
}
func TestOptionalNATExcludesInternalNetworks(t *testing.T) {
	_, _, c := fixture(t)
	c.NAT = true
	c.ExcludeCIDRs = []string{"172.17.0.0/16"}
	p, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(p.NFTables, "ip daddr != { 192.0.2.0/24, 172.17.0.0/16 } masquerade") {
		t.Fatal(p.NFTables)
	}
}
func TestDHCPGroupsAndBypassGateway(t *testing.T) {
	_, _, c := fixture(t)
	c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.200", RangeEnd: "192.0.2.220", Reservations: []DHCPReservation{{MAC: "02:00:00:00:00:01", Address: "192.0.2.205", Group: "direct", Hostname: "download", Gateway: "192.0.2.1"}}}
	p, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"port=0", "option:dns-server,192.0.2.142", "set:group_direct", "tag:device0,option:router,192.0.2.1"} {
		if !strings.Contains(p.DHCPConfig, v) {
			t.Fatal(v)
		}
	}
	if strings.Contains(p.DHCPConfig, "dhcp-authoritative") {
		t.Fatal("must not claim existing DHCP")
	}
}
func TestRejectUnsafeAndInvalidConfig(t *testing.T) {
	for _, what := range []string{"interface", "network", "wide_network", "ipv6", "bypass", "dhcp_name", "range", "dns", "empty"} {
		t.Run(what, func(t *testing.T) {
			_, _, c := fixture(t)
			switch what {
			case "interface":
				c.LANInterface = "eth0\nflush ruleset"
			case "network":
				c.LANCIDRs = []string{"192.0.2.1/24"}
			case "wide_network":
				c.LANCIDRs = []string{"0.0.0.0/0"}
			case "ipv6":
				c.IPv6Mode = "proxy"
			case "bypass":
				c.BypassCIDRs = []string{"0.0.0.0/0"}
			case "dhcp_name":
				c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.200", RangeEnd: "192.0.2.220", Reservations: []DHCPReservation{{MAC: "02:00:00:00:00:01", Address: "192.0.2.205", Hostname: "x\nscript=/bin/sh"}}}
			case "range":
				c.DHCP = DHCPConfig{Enabled: true, RangeStart: "192.0.2.1", RangeEnd: "192.0.2.220"}
			case "dns":
				c.DNSPort = 65536
			case "empty":
				c.LANAddress = ""
			}
			if e := Validate(c); e == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
