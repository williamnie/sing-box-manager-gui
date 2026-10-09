package migration

import (
	"github.com/xiaobei/singbox-manager/internal/storage"
	"reflect"
	"testing"
)

func TestAdoptManagedNodesUsesSubscriptionSourceAndKeepsDedicatedExitEditable(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.ImportedPolicy = &storage.ImportedPolicy{Final: "SMbox/Proxy", Outbounds: []map[string]any{
		{"tag": "SMbox/Proxy", "type": "selector", "outbounds": []string{"SMbox/自建家宽"}},
		{"tag": "SMbox/自建家宽", "type": "selector", "outbounds": []string{"已删节点"}},
		{"tag": "已删节点", "type": "socks", "server": "192.0.2.2", "server_port": 1080},
		{"tag": "SMbox/长沙", "type": "socks", "server": "192.0.2.3", "server_port": 1080, "password": "fixture-only"},
		{"tag": "SMbox/直连", "type": "direct"},
	}, Rules: []map[string]any{
		{"domain_suffix": []string{"example.com"}, "outbound": "SMbox/自建家宽"},
		{"domain": []string{"special.example"}, "outbound": "SMbox/长沙"},
		{"domain_suffix": []string{"cn"}, "outbound": "SMbox/直连"},
		{"domain": []string{"ad.example"}, "action": "reject"},
	}}
	data := &storage.AppData{Settings: settings, Filters: []storage.Filter{{Name: "家庭代理", Enabled: false}}, Rules: []storage.Rule{{Name: "STUN", Outbound: "家庭代理", Protocol: []string{"stun"}, SourceCIDRs: []string{"192.0.2.1"}}}, Subscriptions: []storage.Subscription{{Enabled: true, Nodes: []storage.Node{{Tag: "新节点", Type: "socks", Server: "192.0.2.4", ServerPort: 1080}}}}}
	adopted, err := AdoptManagedNodes(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(adopted, []string{"长沙"}) || len(settings.ImportedPolicy.Outbounds) != 0 {
		t.Fatal(adopted)
	}
	if data.Rules[0].Outbound != "Proxy" || data.Filters[0].Enabled || !reflect.DeepEqual(data.Rules[0].SourceCIDRs, []string{"192.0.2.1"}) {
		t.Fatal("disabled filter reference or match lost")
	}
	for i, want := range []string{"Proxy", "长沙", "DIRECT", ""} {
		got, _ := settings.ImportedPolicy.Rules[i]["outbound"].(string)
		if got != want {
			t.Fatalf("rule %d: %s", i, got)
		}
	}
	if len(data.ManualNodes) != 1 || data.ManualNodes[0].Node.Extra["password"] != "fixture-only" {
		t.Fatal("dedicated node not preserved")
	}
	if again, err := AdoptManagedNodes(data); err != nil || len(again) != 0 || len(data.ManualNodes) != 1 {
		t.Fatal("migration not idempotent")
	}
}
func TestAdoptManagedNodesReusesExactNativeConnection(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.ImportedPolicy = &storage.ImportedPolicy{Outbounds: []map[string]any{{"tag": "old", "type": "socks", "server": "192.0.2.1", "server_port": 1080}}, Rules: []map[string]any{{"outbound": "old", "domain": []string{"example.com"}}}}
	data := &storage.AppData{Settings: settings, Subscriptions: []storage.Subscription{{Enabled: true, Nodes: []storage.Node{{Tag: "native", Type: "socks", Server: "192.0.2.1", ServerPort: 1080}}}}}
	adopted, err := AdoptManagedNodes(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 0 || data.Settings.ImportedPolicy.Rules[0]["outbound"] != "native" {
		t.Fatal("same node duplicated")
	}
}
