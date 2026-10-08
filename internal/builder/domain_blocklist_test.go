package builder

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestCustomDomainBlocklistRejectsInDNSAndTraffic(t *testing.T) {
	for _, mode := range []string{"desktop", "gateway", "dns-bypass"} {
		t.Run(mode, func(t *testing.T) {
			s := storage.DefaultSettings()
			if mode == "gateway" {
				s = gatewaySettings()
			}
			if mode == "dns-bypass" {
				s = dnsBypassSettings()
			}
			rule := storage.Rule{ID: "custom-domain-reject", Name: "自定义域名拦截", RuleType: "domain", Values: []string{"api-access.pangolin-sdk-toutiao1.com", "ads.example"}, Outbound: "REJECT", Enabled: true, Priority: -1}
			b := NewConfigBuilder(s, []storage.Node{{Tag: "node", Type: "socks", Server: "203.0.113.2", ServerPort: 1080}}, nil, []storage.Rule{rule}, []storage.RuleGroup{{ID: "ad-block", Name: "广告拦截", SiteRules: []string{"category-ads-all"}, Outbound: "REJECT", Enabled: true}}).WithPlatform("linux").WithSingBoxVersion("sing-box version 1.14.2")
			config, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			dnsRaw, _ := json.Marshal(config.DNS)
			var dns struct {
				Rules []map[string]any `json:"rules"`
			}
			if err := json.Unmarshal(dnsRaw, &dns); err != nil {
				t.Fatal(err)
			}
			foundDNS, foundRoute := false, false
			for _, r := range dns.Rules {
				if reflect.DeepEqual(r["domain"], []any{rule.Values[0], rule.Values[1]}) {
					foundDNS = r["action"] == "reject" && r["domain_suffix"] == nil && r["source_ip_cidr"] == nil
				}
			}
			for _, r := range config.Route.Rules {
				if reflect.DeepEqual(r["domain"], rule.Values) {
					foundRoute = r["outbound"] == "REJECT" && r["domain_suffix"] == nil
				}
			}
			if !foundDNS || !foundRoute {
				t.Fatalf("exact reject projection missing: DNS=%v route=%v", foundDNS, foundRoute)
			}
			if len(config.Route.RuleSet) != 1 || config.Route.RuleSet[0].Tag != "geosite-category-ads-all" {
				t.Fatal("changed upstream ruleset", config.Route.RuleSet)
			}
		})
	}
}
