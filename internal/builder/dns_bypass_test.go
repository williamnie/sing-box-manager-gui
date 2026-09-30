package builder

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func dnsBypassSettings() *storage.Settings {
	s := gatewaySettings()
	s.Gateway.AccessMode = "dns"
	s.Gateway.StaticRouteConfirmed = true
	return s
}

func TestDNSBypassScopeAndPersistence(t *testing.T) {
	s := dnsBypassSettings()
	s.ClashAPIPort = 0
	c, err := gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range c.Inbounds {
		if in.Type == "tun" || in.AutoRoute || in.AutoRedirect {
			t.Fatal("DNS mode captured global routing")
		}
	}
	if len(c.Inbounds) != 3 || c.Inbounds[1].Tag != "lan-dns" || c.Inbounds[2].Type != "tproxy" {
		t.Fatal(c.Inbounds)
	}
	if c.DNS.Raw["disable_cache"] != true || c.Experimental == nil || !c.Experimental.CacheFile.StoreFakeIP {
		t.Fatal("missing cache isolation or persistence without Clash API")
	}
	path := c.Experimental.CacheFile.Path
	s.Gateway.FakeIPRange = "198.19.0.0/16"
	c, err = gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	if path == c.Experimental.CacheFile.Path {
		t.Fatal("changed pool reuses incompatible mapping")
	}
	if c.Experimental.ClashAPI != nil {
		t.Fatal("enabled disabled controller")
	}
}

func TestDNSBypassNoProjectionOfTrafficConditions(t *testing.T) {
	rules := []storage.Rule{
		{Name: "web-only", Enabled: true, RuleType: "domain", Values: []string{"limited.example"}, Ports: []int{443}, Outbound: "DIRECT"},
		{Name: "STUN", Enabled: true, RuleType: "match", Protocol: []string{"stun"}, Outbound: "REJECT"},
		{Name: "domestic", Enabled: true, RuleType: "domain_suffix", Values: []string{"direct.example"}, Outbound: "DIRECT"},
	}
	c, err := gwBuilder(dnsBypassSettings(), rules).Build()
	if err != nil {
		t.Fatal(err)
	}
	dns, _ := json.Marshal(c.DNS)
	if strings.Contains(string(dns), "limited.example") || strings.Contains(string(dns), "stun") {
		t.Fatal("traffic-only rule broadened into DNS")
	}
	if !strings.Contains(string(dns), "direct.example") {
		t.Fatal("lost direct domain rule")
	}
	route, _ := json.Marshal(c.Route)
	if !strings.Contains(string(route), "limited.example") || !strings.Contains(string(route), "stun") {
		t.Fatal("lost traffic rules")
	}
}

func TestDNSBypassRejectsUnidentifiableAndStrictPolicy(t *testing.T) {
	s := dnsBypassSettings()
	s.DeviceGroups = []storage.DeviceGroup{{ID: "group", Policy: "strict"}}
	if _, err := gwBuilder(s, nil).Build(); err == nil {
		t.Fatal("DNS mode promised strict coverage")
	}
	s.DeviceGroups[0].Policy = "direct"
	s.Devices = []storage.Device{{ID: "d", Enabled: true, GroupID: "group", Addresses: []string{"192.0.2.10"}}}
	s.Gateway.DNSSource = "router"
	if _, err := gwBuilder(s, nil).Build(); err == nil {
		t.Fatal("router DNS was treated as end device")
	}
	s.Devices = nil
	r := storage.Rule{Enabled: true, RuleType: "domain", Values: []string{"example.org"}, SourceCIDRs: []string{"192.0.2.10"}, Outbound: "DIRECT"}
	if _, err := gwBuilder(s, []storage.Rule{r}).Build(); err == nil {
		t.Fatal("source rule accepted behind forwarding DNS")
	}
	if _, err := gwBuilder(s, nil).Build(); err != nil {
		t.Fatal("global policy should support forwarding DNS", err)
	}
}

func TestDNSBypassImportedPolicyPreservedAndSafelyProjected(t *testing.T) {
	s := dnsBypassSettings()
	s.ImportedPolicy = &storage.ImportedPolicy{
		Outbounds: []map[string]any{{"type": "direct", "tag": "DIRECT"}, {"type": "socks", "tag": "Proxy", "server": "203.0.113.8", "server_port": 1080}},
		Rules:     []map[string]any{{"domain_suffix": []string{"imported.example"}, "outbound": "DIRECT"}, {"domain": []string{"limited.example"}, "port": 443, "outbound": "DIRECT"}}, Final: "Proxy",
		DNS: map[string]any{"servers": []any{map[string]any{"type": "udp", "tag": "local-resolver", "server": "192.0.2.1"}}, "rules": []any{map[string]any{"domain_suffix": []string{"lan"}, "server": "local-resolver"}}},
	}
	before := deepMap(map[string]any{"policy": s.ImportedPolicy})
	c, err := gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, deepMap(map[string]any{"policy": s.ImportedPolicy})) {
		t.Fatal("mutated original import")
	}
	dns, _ := json.Marshal(c.DNS)
	if !strings.Contains(string(dns), "imported.example") || strings.Contains(string(dns), "limited.example") {
		t.Fatal(string(dns))
	}
	s.ImportedPolicy.DNS["rules"] = []any{map[string]any{"network": "udp", "server": "local-resolver"}}
	if _, err := gwBuilder(s, nil).Build(); err == nil {
		t.Fatal("unrepresentable DNS silently widened")
	}
}

func TestDNSBypassBypassDeviceStillResolvesStaleFakeIP(t *testing.T) {
	s := dnsBypassSettings()
	s.DeviceGroups = []storage.DeviceGroup{{ID: "bt", Policy: "bypass"}}
	s.Devices = []storage.Device{{ID: "d", Enabled: true, GroupID: "bt", Addresses: []string{"192.0.2.10"}}}
	c, err := gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range c.Route.Rules {
		if r["source_ip_cidr"] != nil && r["action"] == "bypass" {
			t.Fatal("stale FakeIP escaped without resolution")
		}
	}
}

func TestDNSBypassImportedCatchAllAndScopedHosts(t *testing.T) {
	s := dnsBypassSettings()
	s.ImportedPolicy = &storage.ImportedPolicy{
		Outbounds: []map[string]any{{"type": "direct", "tag": "DIRECT"}, {"type": "socks", "tag": "Proxy", "server": "203.0.113.9", "server_port": 1080}},
		Rules:     []map[string]any{{"outbound": "Proxy"}}, Final: "DIRECT",
		DNS: map[string]any{"servers": []any{map[string]any{"type": "hosts", "tag": "scoped-hosts", "predefined": map[string]any{"private.test": "192.0.2.55"}}}, "rules": []any{map[string]any{"source_ip_cidr": []string{"192.0.2.10"}, "domain": []string{"private.test"}, "server": "scoped-hosts"}}},
	}
	c, err := gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c.DNS)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	fakeBeforeFinal := false
	for _, item := range doc["rules"].([]any) {
		r := item.(map[string]any)
		if r["server"] == "scoped-hosts" && r["source_ip_cidr"] == nil {
			t.Fatal("imported hosts escaped source constraint")
		}
		if r["server"] == "dns_fakeip" {
			fakeBeforeFinal = true
		}
	}
	if !fakeBeforeFinal {
		t.Fatal("catch-all Proxy rule discarded in favor of DIRECT final")
	}
}
