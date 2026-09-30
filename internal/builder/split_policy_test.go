package builder

import (
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestSplitDeviceWithIndependentSTUNPolicy(t *testing.T) {
	for _, stunOutbound := range []string{"Proxy", "REJECT"} {
		t.Run(stunOutbound, func(t *testing.T) {
			s := gatewaySettings()
			s.DeviceGroups = []storage.DeviceGroup{{ID: "normal", Policy: "split"}, {ID: "strict", Policy: "strict", Outbound: "SMbox/node"}}
			s.Devices = []storage.Device{{ID: "workstation", GroupID: "normal", Addresses: []string{"192.0.2.10"}, Enabled: true}, {ID: "strict-device", GroupID: "strict", Addresses: []string{"192.0.2.11"}, Enabled: true}}
			s.ImportedPolicy = &storage.ImportedPolicy{
				Outbounds: []map[string]any{{"type": "direct", "tag": "DIRECT"}, {"type": "socks", "tag": "SMbox/node", "server": "203.0.113.8", "server_port": 1080}, {"type": "selector", "tag": "Proxy", "outbounds": []string{"SMbox/node"}}},
				Rules:     []map[string]any{{"domain_suffix": []string{"baidu.com"}, "outbound": "DIRECT"}, {"protocol": "bittorrent", "outbound": "DIRECT"}, {"port_range": []string{"6881:60000"}, "outbound": "DIRECT"}}, Final: "Proxy",
			}
			before := deepMap(map[string]any{"policy": s.ImportedPolicy})
			rules := []storage.Rule{
				{Name: "STUN for workstation", RuleType: "match", Enabled: true, SourceCIDRs: []string{"192.0.2.10"}, Protocol: []string{"stun"}, Outbound: stunOutbound, Priority: -1},
				{Name: "new subscription route", RuleType: "domain", Values: []string{"subscription.example"}, Enabled: true, Outbound: "Managed Proxy", Priority: 100},
			}
			c, err := gwBuilder(s, rules).Build()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, deepMap(map[string]any{"policy": s.ImportedPolicy})) {
				t.Fatal("building rewrote legacy imported policy")
			}
			if !reflect.DeepEqual(c.Route.Rules[len(c.Route.Rules)-3:], []RouteRule{RouteRule(s.ImportedPolicy.Rules[0]), RouteRule(s.ImportedPolicy.Rules[1]), RouteRule(s.ImportedPolicy.Rules[2])}) {
				t.Fatal("import order changed")
			}
			for _, r := range c.Route.Rules {
				if reflect.DeepEqual(stringsFrom(r["protocol"]), []string{"stun"}) {
					if r["source_ip_cidr"] == nil || r["port"] != nil || r["port_range"] != nil || r["network"] != nil {
						t.Fatal("STUN rule lost scope or became port-dependent")
					}
				}
			}
			for _, tc := range []struct {
				name, source, domain, protocol string
				port                           int
				want                           string
			}{
				{"domestic", "192.0.2.10", "www.baidu.com", "tls", 443, "DIRECT"},
				{"ordinary proxy", "192.0.2.10", "example.org", "tls", 443, "Proxy"},
				{"STUN before wide imported ports", "192.0.2.10", "", "stun", 19302, stunOutbound},
				{"STUN arbitrary port", "192.0.2.10", "", "stun", 65530, stunOutbound},
				{"BT on STUN-associated port", "192.0.2.10", "", "bittorrent", 3478, "DIRECT"},
				{"unrelated UDP unchanged", "192.0.2.10", "", "quic", 19302, "DIRECT"},
				{"other source unchanged", "192.0.2.20", "", "stun", 19302, "DIRECT"},
				{"explicit strict retained", "192.0.2.11", "www.baidu.com", "tls", 443, "SMbox/node"},
				{"new subscription route", "192.0.2.10", "subscription.example", "tls", 443, "Managed Proxy"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got := c.Route.Final
					for _, r := range c.Route.Rules {
						if fixtureMatches(t, r, tc.source, tc.domain, tc.protocol, tc.port) {
							got, _ = r["outbound"].(string)
							break
						}
					}
					if got != tc.want {
						t.Fatalf("got %s, want %s", got, tc.want)
					}
				})
			}
		})
	}
}

// 仅解释此用例涉及的简单条件，验证组合后的首个路由结果；不代替真实内核验收。
func fixtureMatches(t *testing.T, r RouteRule, source, domain, protocol string, port int) bool {
	t.Helper()
	if r["action"] != nil {
		return false
	}
	for key, v := range r {
		matches := false
		switch key {
		case "outbound":
			continue
		case "source_ip_cidr":
			for _, s := range stringsFrom(v) {
				p, err := storage.SourcePrefix(s)
				if err != nil {
					t.Fatal(err)
				}
				matches = matches || p.Contains(netip.MustParseAddr(source))
			}
		case "protocol", "domain", "domain_suffix":
			for _, s := range stringsFrom(v) {
				if key == "protocol" {
					matches = matches || s == protocol
				} else {
					matches = matches || s == domain || (key == "domain_suffix" && strings.HasSuffix(domain, "."+s))
				}
			}
		case "port_range":
			for _, s := range stringsFrom(v) {
				parts := strings.Split(s, ":")
				a, _ := strconv.Atoi(parts[0])
				b, _ := strconv.Atoi(parts[1])
				matches = matches || (port >= a && port <= b)
			}
		default:
			t.Fatalf("fixture matcher does not cover %s", key)
		}
		if !matches {
			return false
		}
	}
	return true
}
