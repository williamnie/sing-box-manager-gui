package builder

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestCompactProxyPlanRemovesAutomaticGroups(t *testing.T) {
	settings := storage.DefaultSettings()
	if err := json.Unmarshal([]byte(`{"proxy_plan":{"primary":"家庭代理","merge_groups":[]}}`), settings); err != nil {
		t.Fatal(err)
	}
	b := NewConfigBuilder(settings, []storage.Node{{Tag: "node", Type: "socks", Server: "192.0.2.1", ServerPort: 1080, Country: "US"}}, []storage.Filter{{Name: "家庭代理", Mode: "selector", Enabled: true, AllNodes: true}}, nil, storage.DefaultRuleGroups())
	config, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	groups := []string{}
	for _, o := range config.Outbounds {
		if o["type"] == "selector" || o["type"] == "urltest" {
			groups = append(groups, o["tag"].(string))
		}
	}
	if len(groups) != 1 || groups[0] != "家庭代理" {
		t.Fatalf("want only chosen group, got %v", groups)
	}
	if config.Route.Final != "家庭代理" {
		t.Fatalf("wrong final %q", config.Route.Final)
	}
}

func TestCompactProxyPlanMergesReferencesWithoutChangingSource(t *testing.T) {
	for _, mode := range []string{"desktop", "dns"} {
		t.Run(mode, func(t *testing.T) {
			settings := storage.DefaultSettings()
			if mode == "dns" {
				settings = dnsBypassSettings()
			}
			settings.ProxyPlan = &storage.ProxyPlan{Primary: "家庭代理", MergeGroups: []string{"Proxy", "旧默认", "旧家宽"}}
			settings.ImportedPolicy = &storage.ImportedPolicy{Final: "旧默认", Outbounds: []map[string]any{
				{"type": "socks", "tag": "已删旧节点", "server": "192.0.2.2", "server_port": 1080},
				{"type": "socks", "tag": "地区节点", "server": "192.0.2.3", "server_port": 1080},
				{"type": "socks", "tag": "下载专用", "server": "192.0.2.4", "server_port": 1080},
				{"type": "selector", "tag": "旧家宽", "outbounds": []string{"已删旧节点"}},
				{"type": "selector", "tag": "旧默认", "outbounds": []string{"旧家宽"}},
				{"type": "selector", "tag": "Proxy", "outbounds": []string{"旧默认"}},
				{"type": "selector", "tag": "专用地区", "outbounds": []string{"地区节点", "旧默认"}, "default": "旧默认"},
			}, Rules: []map[string]any{
				{"domain_suffix": []string{"example.com"}, "outbound": "旧家宽"},
				{"domain_suffix": []string{"region.example"}, "outbound": "专用地区"},
				{"domain_suffix": []string{"direct.example"}, "outbound": "DIRECT"},
				{"domain_suffix": []string{"blocked.example"}, "action": "reject"},
			}, RuleSets: []map[string]any{{"tag": "extra", "type": "remote", "format": "binary", "url": "https://example.com/r.srs", "download_detour": "下载专用"}}, DNS: map[string]any{"servers": []any{map[string]any{"type": "https", "tag": "old-dns", "server": "1.1.1.1", "detour": "旧默认"}}}}
			source, _ := json.Marshal(settings)
			b := NewConfigBuilder(settings, []storage.Node{{Tag: "新节点", Type: "socks", Server: "192.0.2.5", ServerPort: 1080}}, []storage.Filter{{Name: "家庭代理", Mode: "selector", Enabled: true, AllNodes: true}}, nil, nil).WithPlatform("linux").WithSingBoxVersion("1.14.2")
			config, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(settings)
			if string(after) != string(source) {
				t.Fatal("source settings were mutated")
			}
			tags := map[string]bool{}
			for _, o := range config.Outbounds {
				tags[o["tag"].(string)] = true
			}
			for _, tag := range []string{"家庭代理", "新节点", "地区节点", "专用地区", "下载专用", "DIRECT", "REJECT"} {
				if !tags[tag] {
					t.Errorf("lost %s", tag)
				}
			}
			for _, tag := range []string{"已删旧节点", "Proxy", "旧默认", "旧家宽", "Managed Auto", "Managed Proxy", "Managed Final"} {
				if tags[tag] {
					t.Errorf("unexpected %s", tag)
				}
			}
			if config.Route.Final != "家庭代理" {
				t.Fatal(config.Route.Final)
			}
			raw, _ := json.Marshal(config)
			for _, old := range []string{`"旧默认"`, `"旧家宽"`, `"已删旧节点"`} {
				if strings.Contains(string(raw), old) {
					t.Errorf("stale reference %s", old)
				}
			}
			for _, o := range config.Outbounds {
				if o["tag"] == "专用地区" && o["default"] != "家庭代理" {
					t.Fatal("default not redirected", o)
				}
			}
			again, err := b.BuildJSON()
			if err != nil {
				t.Fatal(err)
			}
			var got any
			_ = json.Unmarshal([]byte(again), &got)
			var expected any
			_ = json.Unmarshal(raw, &expected)
			if !reflect.DeepEqual(got, expected) {
				t.Fatal("repeated build changed plan")
			}
			settings.ProxyPlan = nil
			original, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			restored := false
			for _, o := range original.Outbounds {
				if o["tag"] == "已删旧节点" {
					restored = true
				}
			}
			if !restored || original.Route.Final != "旧默认" {
				t.Fatal("cannot restore original layout")
			}
		})
	}
}

func TestCompactProxyPlanRejectsInvalidPrimaryAndLeafMerge(t *testing.T) {
	for _, plan := range []*storage.ProxyPlan{{Primary: "不存在"}, {Primary: "家庭代理", MergeGroups: []string{"旧节点"}}} {
		settings := storage.DefaultSettings()
		settings.ProxyPlan = plan
		settings.ImportedPolicy = &storage.ImportedPolicy{Final: "旧节点", Outbounds: []map[string]any{{"type": "socks", "tag": "旧节点", "server": "192.0.2.1", "server_port": 1080}}}
		_, err := NewConfigBuilder(settings, []storage.Node{{Tag: "new", Type: "socks", Server: "192.0.2.2", ServerPort: 1080}}, []storage.Filter{{Name: "家庭代理", Mode: "selector", Enabled: true}}, nil, nil).Build()
		if err == nil {
			t.Fatal("invalid plan accepted", plan)
		}
	}
}

func TestCompactProxyPlanKeepsExplicitAutomaticFilterAndDetour(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.ProxyPlan = &storage.ProxyPlan{Primary: "家庭代理"}
	nodes := []storage.Node{{Tag: "出口", Type: "socks", Server: "192.0.2.1", ServerPort: 1080, Extra: map[string]any{"detour": "上游"}}, {Tag: "上游", Type: "socks", Server: "192.0.2.2", ServerPort: 1080}, {Tag: "不使用", Type: "socks", Server: "192.0.2.3", ServerPort: 1080}}
	filters := []storage.Filter{{Name: "家庭代理", Enabled: true, Mode: "urltest", NodeTags: []string{"出口"}}}
	config, err := NewConfigBuilder(settings, nodes, filters, nil, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]bool{}
	for _, o := range config.Outbounds {
		tags[o["tag"].(string)] = true
		if o["tag"] == "家庭代理" && o["type"] != "urltest" {
			t.Fatal("explicit automatic mode lost")
		}
	}
	if !tags["上游"] || tags["不使用"] || tags["Auto"] {
		t.Fatal(tags)
	}
}

func TestRealKernelCompactProxyPlan(t *testing.T) {
	binary := os.Getenv("SBM_TEST_SINGBOX")
	if binary == "" {
		t.Skip("set SBM_TEST_SINGBOX for real kernel validation")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"desktop", "dns"} {
		t.Run(mode, func(t *testing.T) {
			settings := storage.DefaultSettings()
			platform := "darwin"
			if mode == "dns" {
				settings = dnsBypassSettings()
				platform = "linux"
			}
			settings.ProxyPlan = &storage.ProxyPlan{Primary: "家庭代理", MergeGroups: []string{"Proxy"}}
			settings.ImportedPolicy = &storage.ImportedPolicy{Final: "Proxy", Outbounds: []map[string]any{{"type": "selector", "tag": "Proxy", "outbounds": []string{"old"}}, {"type": "socks", "tag": "old", "server": "192.0.2.3", "server_port": 1080}}, Rules: []map[string]any{{"domain_suffix": []string{"example.com"}, "outbound": "Proxy"}, {"domain_suffix": []string{"blocked.example"}, "action": "reject"}}}
			b := NewConfigBuilder(settings, []storage.Node{{Tag: "new", Type: "socks", Server: "192.0.2.4", ServerPort: 1080}}, []storage.Filter{{Name: "家庭代理", Enabled: true, Mode: "selector", AllNodes: true}}, nil, nil).WithPlatform(platform).WithSingBoxVersion(string(version))
			raw, err := b.BuildJSON()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			action := "check"
			if mode == "dns" && runtime.GOOS != "linux" {
				action = "format"
				t.Log("non-Linux host: DNS bypass parsing only")
			}
			cmd := exec.Command(binary, action, "-c", path)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("kernel rejected plan: %v\n%s", err, out)
			}
		})
	}
}

func TestManagedDefaultProxyFollowsEnabledNodes(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.ProxyPlan = &storage.ProxyPlan{Primary: "Proxy", ManagedOnly: true, DefaultNode: "removed"}
	settings.ImportedPolicy = &storage.ImportedPolicy{Final: "Proxy"}
	nodes := []storage.Node{{Tag: "node", Type: "socks", Server: "192.0.2.1", ServerPort: 1080, Country: "US"}}
	filters := []storage.Filter{{Name: "家庭代理", Mode: "selector", Enabled: false, AllNodes: true}}
	b := NewConfigBuilder(settings, nodes, filters, nil, nil)
	config, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	groups := []string{}
	for _, out := range config.Outbounds {
		if out["type"] == "selector" || out["type"] == "urltest" {
			groups = append(groups, out["tag"].(string))
			if out["tag"] != "GLOBAL" && out["default"] != "node" {
				t.Fatal("stale preferred node retained")
			}
		}
	}
	if !reflect.DeepEqual(groups, []string{"Proxy", "🇺🇸 美国", "GLOBAL"}) {
		t.Fatal(groups)
	}
	b.nodes = nil
	config, err = b.Build()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, out := range config.Outbounds {
		if out["tag"] == "Proxy" {
			found = true
			if out["type"] != "block" {
				t.Fatal("empty node source must not silently go direct")
			}
		}
	}
	if !found {
		t.Fatal("missing fail-closed default")
	}
}

func TestRealKernelManagedDefaultProxy(t *testing.T) {
	binary := os.Getenv("SBM_TEST_SINGBOX")
	if binary == "" {
		t.Skip("set SBM_TEST_SINGBOX for real kernel validation")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, empty := range []bool{false, true} {
		settings := dnsBypassSettings()
		settings.ProxyPlan = &storage.ProxyPlan{Primary: "Proxy", ManagedOnly: true, DefaultNode: "node"}
		settings.ImportedPolicy = &storage.ImportedPolicy{Final: "Proxy", Rules: []map[string]any{{"domain": []string{"example.com"}, "outbound": "Proxy"}, {"domain": []string{"blocked.example"}, "action": "reject"}}}
		nodes := []storage.Node{{Tag: "node", Type: "socks", Server: "192.0.2.1", ServerPort: 1080}}
		if empty {
			nodes = nil
		}
		raw, err := NewConfigBuilder(settings, nodes, nil, nil, nil).WithPlatform("linux").WithSingBoxVersion(string(version)).BuildJSON()
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		action := "check"
		if runtime.GOOS != "linux" {
			action = "format"
		}
		cmd := exec.Command(binary, action, "-c", path)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("kernel rejected empty=%v: %v\n%s", empty, err, out)
		}
	}
}
