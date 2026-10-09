package builder

import (
	"github.com/xiaobei/singbox-manager/internal/storage"
	"reflect"
	"slices"
	"testing"
)

func TestManagedGroupsExposeGlobalProxyCountriesAndEnabledFilters(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.ProxyPlan = &storage.ProxyPlan{Primary: "Proxy", ManagedOnly: true, DefaultNode: "US-2"}
	settings.ImportedPolicy = &storage.ImportedPolicy{Final: "Proxy"}
	nodes := []storage.Node{{Tag: "US-1", Country: "US", Type: "socks", Server: "192.0.2.1", ServerPort: 1080}, {Tag: "US-2", Country: "US", Type: "socks", Server: "192.0.2.2", ServerPort: 1080}, {Tag: "JP-1", Country: "JP", Type: "socks", Server: "192.0.2.3", ServerPort: 1080}, {Tag: "unknown", Type: "socks", Server: "192.0.2.4", ServerPort: 1080}}
	filters := []storage.Filter{{Name: "enabled", Enabled: true, Mode: "selector", NodeTags: []string{"US-2"}}, {Name: "disabled", Enabled: false, Mode: "selector", AllNodes: true}}
	rules := []storage.Rule{{ID: "region", Name: "日本网站", RuleType: "domain_suffix", Values: []string{"example.jp"}, Outbound: "🇯🇵 日本", Enabled: true}}
	config, err := NewConfigBuilder(settings, nodes, filters, rules, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	byTag := map[string]Outbound{}
	for _, o := range config.Outbounds {
		byTag[o["tag"].(string)] = o
	}
	if !reflect.DeepEqual(byTag["Proxy"]["outbounds"], []string{"US-1", "US-2", "JP-1", "unknown"}) {
		t.Fatal("Proxy must contain only enabled nodes", byTag["Proxy"])
	}
	if !reflect.DeepEqual(byTag["🇺🇸 美国"]["outbounds"], []string{"US-1", "US-2"}) || byTag["🇺🇸 美国"]["default"] != "US-2" {
		t.Fatal("missing US group", byTag["🇺🇸 美国"])
	}
	if !reflect.DeepEqual(byTag["🇯🇵 日本"]["outbounds"], []string{"JP-1"}) || !reflect.DeepEqual(byTag["🌐 其他"]["outbounds"], []string{"unknown"}) {
		t.Fatal("country membership incorrect")
	}
	if byTag["GLOBAL"] == nil || byTag["GLOBAL"]["type"] != "selector" || byTag["GLOBAL"]["default"] != "Proxy" {
		t.Fatal("GLOBAL must be a real selector", byTag["GLOBAL"])
	}
	want := []string{"DIRECT", "REJECT", "Proxy", "🇺🇸 美国", "🇯🇵 日本", "🌐 其他", "enabled", "US-1", "US-2", "JP-1", "unknown"}
	got := stringsFrom(byTag["GLOBAL"]["outbounds"])
	slices.Sort(want)
	slices.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("GLOBAL contents incorrect", got)
	}
	if byTag["disabled"] != nil || byTag["Auto"] != nil || byTag["Managed Proxy"] != nil {
		t.Fatal("unexpected extra group")
	}
	if config.Route.Final != "Proxy" {
		t.Fatal("default route must stay unchanged")
	}
	found := false
	for _, r := range config.Route.Rules {
		if r["outbound"] == "🇯🇵 日本" {
			found = true
		}
	}
	if !found {
		t.Fatal("country rule silently redirected to Proxy")
	}
}

func TestManagedGroupsDropRemovedCountriesAndNodes(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.ProxyPlan = &storage.ProxyPlan{Primary: "Proxy", ManagedOnly: true}
	config, err := NewConfigBuilder(settings, []storage.Node{{Tag: "only", Country: "US", Type: "socks", Server: "192.0.2.1", ServerPort: 1080}}, nil, nil, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range config.Outbounds {
		if o["tag"] == "🇯🇵 日本" || o["tag"] == "🌐 其他" {
			t.Fatal("empty country remained")
		}
	}
}
