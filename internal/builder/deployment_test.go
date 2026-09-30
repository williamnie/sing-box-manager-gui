package builder

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/gateway"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

func gatewaySettings() *storage.Settings {
	s := storage.DefaultSettings()
	s.DeploymentRole = "gateway"
	s.Gateway = gateway.Config{LANInterface: "eth0", UplinkInterface: "eth0", LANAddress: "192.0.2.2", LANCIDRs: []string{"192.0.2.0/24"}, UpstreamGateway: "192.0.2.1", IPv6Mode: "disabled"}
	return s
}
func gwBuilder(s *storage.Settings, rules []storage.Rule) *ConfigBuilder {
	return NewConfigBuilder(s, []storage.Node{{Tag: "node", Type: "socks", Server: "203.0.113.2", ServerPort: 1080}}, nil, rules, nil).WithPlatform("linux").WithSingBoxVersion("sing-box version 1.14.1")
}
func TestGatewayRulePrecedenceAndSourceConjunction(t *testing.T) {
	s := gatewaySettings()
	s.DeviceGroups = []storage.DeviceGroup{{ID: "strict", Policy: "strict"}}
	s.Devices = []storage.Device{{ID: "mbp", Enabled: true, GroupID: "strict", Addresses: []string{"192.0.2.177"}}}
	rules := []storage.Rule{{ID: "pt", Name: "PT only", Enabled: true, RuleType: "port_range", Values: []string{"6881:60000", "411:413"}, SourceCIDRs: []string{"192.0.2.205"}, Network: []string{"udp"}, Protocol: []string{"bittorrent"}, Outbound: "DIRECT"}}
	c, err := gwBuilder(s, rules).Build()
	if err != nil {
		t.Fatal(err)
	}
	strictIndex, ptIndex := -1, -1
	for i, r := range c.Route.Rules {
		if r["outbound"] == "Proxy" {
			strictIndex = i
		}
		if r["port_range"] != nil {
			ptIndex = i
			for _, key := range []string{"source_ip_cidr", "network", "protocol"} {
				if r[key] == nil {
					t.Fatalf("missing conjunction %s", key)
				}
			}
		}
	}
	if strictIndex < 0 || ptIndex <= strictIndex {
		t.Fatalf("strict=%d pt=%d", strictIndex, ptIndex)
	}
}
func TestDNSUsesSettingsAndDirectDevicesNeverFakeIP(t *testing.T) {
	s := gatewaySettings()
	s.ProxyDNS = "tls://9.9.9.9:853"
	s.DirectDNS = "https://dns.example/dns-query"
	s.DeviceGroups = []storage.DeviceGroup{{ID: "d", Policy: "direct"}, {ID: "b", Policy: "bypass"}}
	s.Devices = []storage.Device{{ID: "1", GroupID: "d", Addresses: []string{"192.0.2.3"}, Enabled: true}, {ID: "2", GroupID: "b", Addresses: []string{"192.0.2.4"}, Enabled: true}}
	c, err := gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	if c.DNS.Servers[0].Server != "9.9.9.9" || c.DNS.Servers[0].Type != "tls" || c.DNS.Servers[1].DomainResolver != "dns_bootstrap" {
		t.Fatalf("DNS settings ignored: %#v", c.DNS.Servers)
	}
	for _, server := range c.DNS.Servers {
		if server.Type == "fakeip" {
			t.Fatal("gateway unexpectedly uses FakeIP")
		}
	}
	for _, rule := range c.DNS.Rules[:2] {
		if rule.Server != "dns_direct" || len(rule.SourceCIDRs) == 0 {
			t.Fatalf("direct/bypass DNS=%#v", rule)
		}
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "auto_redirect_tproxy_mark") || strings.Contains(string(raw), "multi_queue") {
		t.Fatal("1.15-only field")
	}
	if len(c.Inbounds) != 3 || !c.Inbounds[1].AutoRedirect || c.Inbounds[2].Tag != "lan-dns" {
		t.Fatal("missing gateway ingress")
	}
}
func TestGatewayRejectsUnknownVersionAndMac(t *testing.T) {
	s := gatewaySettings()
	b := NewConfigBuilder(s, nil, nil, nil, nil).WithPlatform("linux")
	if _, err := b.Build(); err == nil {
		t.Fatal("unknown kernel allowed")
	}
	b.WithSingBoxVersion("1.13.19")
	if _, err := b.Build(); err == nil {
		t.Fatal("old kernel allowed")
	}
	b.WithSingBoxVersion("1.14.1").WithPlatform("darwin")
	if _, err := b.Build(); err == nil {
		t.Fatal("Mac gateway allowed")
	}
}
func TestDesktopPreservesMacTUNAndNoLinuxOperations(t *testing.T) {
	s := storage.DefaultSettings()
	c, err := NewConfigBuilder(s, nil, nil, nil, nil).WithPlatform("darwin").Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Inbounds) != 2 || c.Inbounds[1].AutoRedirect || !c.Inbounds[1].AutoRoute || !c.Inbounds[1].StrictRoute {
		t.Fatal("Mac TUN changed")
	}
	if c.Inbounds[0].Listen != "127.0.0.1" {
		t.Fatal("unexpected LAN bind")
	}
}
func TestStrictProxyRejectsDirectInNestedSelector(t *testing.T) {
	s := gatewaySettings()
	s.DeviceGroups = []storage.DeviceGroup{{ID: "s", Policy: "strict", Outbound: "Final"}}
	if _, err := gwBuilder(s, nil).Build(); err == nil {
		t.Fatal("strict can reach DIRECT through Final")
	}
}
func TestGatewayRejectsRemoteProcessAndBadPort(t *testing.T) {
	for _, r := range []storage.Rule{{RuleType: "process_name", Values: []string{"qbittorrent"}}, {RuleType: "port", Values: []string{"6881:60000"}}} {
		r.Enabled = true
		r.Outbound = "DIRECT"
		if _, err := gwBuilder(gatewaySettings(), []storage.Rule{r}).Build(); err == nil {
			t.Fatalf("accepted unsafe rule %#v", r)
		}
	}
}
func TestEqualPriorityKeepsInputOrder(t *testing.T) {
	rules := []storage.Rule{{Name: "one", Enabled: true, RuleType: "domain", Values: []string{"one.test"}, Outbound: "DIRECT"}, {Name: "two", Enabled: true, RuleType: "domain", Values: []string{"two.test"}, Outbound: "Proxy"}}
	c, err := gwBuilder(gatewaySettings(), rules).Build()
	if err != nil {
		t.Fatal(err)
	}
	var domains []string
	for _, r := range c.Route.Rules {
		if a, ok := r["domain"].([]string); ok {
			domains = append(domains, a...)
		}
	}
	if strings.Join(domains, ",") != "one.test,two.test" {
		t.Fatal(domains)
	}
}

func TestImportedPolicyKeepsNewRulesNodesAndStrictDNS(t *testing.T) {
	s := gatewaySettings()
	s.DeviceGroups = []storage.DeviceGroup{{ID: "s", Policy: "strict", Outbound: "safe"}}
	s.Devices = []storage.Device{{ID: "1", GroupID: "s", Addresses: []string{"192.0.2.177"}, Enabled: true}}
	s.SplitDNS = []storage.SplitDNSRule{{ID: "lan", DomainSuffix: []string{"lan"}, Server: "direct"}}
	s.ImportedPolicy = &storage.ImportedPolicy{Final: "Proxy", Outbounds: []map[string]any{{"tag": "DIRECT", "type": "direct"}, {"tag": "Proxy", "type": "selector", "outbounds": []string{"DIRECT", "safe"}}, {"tag": "safe", "type": "socks", "server": "203.0.113.10", "server_port": 1080}}, DNS: map[string]any{"servers": []any{map[string]any{"type": "udp", "tag": "original", "server": "9.9.9.9"}}, "final": "original", "independent_cache": false}}
	b := gwBuilder(s, []storage.Rule{{Name: "custom", Enabled: true, RuleType: "geosite", Values: []string{"github"}, Outbound: "safe"}})
	c, e := b.Build()
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Route.RuleSet) == 0 {
		t.Fatal("new native rule set discarded")
	}
	tags := map[string]bool{}
	for _, o := range c.Outbounds {
		tags[o["tag"].(string)] = true
	}
	if !tags["node"] || !tags["Managed Proxy"] {
		t.Fatal("new native nodes discarded")
	}
	raw, _ := json.Marshal(c.DNS)
	var dns map[string]any
	_ = json.Unmarshal(raw, &dns)
	if dns["independent_cache"] != true {
		t.Fatal("cache isolation discarded")
	}
	foundRule, foundStrict := false, false
	for _, v := range dns["rules"].([]any) {
		r := v.(map[string]any)
		if r["domain_suffix"] != nil && r["server"] == "dns_direct" {
			foundRule = true
		}
	}
	for _, v := range dns["servers"].([]any) {
		r := v.(map[string]any)
		if r["tag"] == "dns_strict_s" && r["detour"] == "safe" {
			foundStrict = true
		}
	}
	if !foundRule || !foundStrict {
		t.Fatalf("split=%v strictDNS=%v %s", foundRule, foundStrict, raw)
	}
}
func TestImportedRemoteProcessConditionRejected(t *testing.T) {
	s := gatewaySettings()
	s.ImportedPolicy = &storage.ImportedPolicy{Rules: []map[string]any{{"type": "logical", "rules": []any{map[string]any{"process_name": "qbittorrent"}}}}}
	if _, err := gwBuilder(s, nil).Build(); err == nil {
		t.Fatal("remote process condition accepted")
	}
}
