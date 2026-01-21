package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestRuleSetService_CollectNeededRuleSets(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "ruleset-test")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// 创建临时存储
	store, err := storage.NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	svc := NewRuleSetService(store, tmpDir)

	ruleGroups := []storage.RuleGroup{
		{ID: "1", Name: "Google", SiteRules: []string{"google"}, IPRules: []string{"google"}, Enabled: true},
		{ID: "2", Name: "CN", SiteRules: []string{"cn"}, Enabled: false}, // 禁用的不应该收集
	}

	rules := []storage.Rule{
		{ID: "1", RuleType: "geosite", Values: []string{"github"}, Enabled: true},
		{ID: "2", RuleType: "geoip", Values: []string{"cn"}, Enabled: true},
		{ID: "3", RuleType: "domain", Values: []string{"example.com"}, Enabled: true}, // 非 geo 类型不应该收集
	}

	needed := svc.collectNeededRuleSets(ruleGroups, rules)

	// 应该收集到: geosite-google, geoip-google, geosite-github, geoip-cn
	expected := map[string]bool{
		"geosite-google": true,
		"geoip-google":   true,
		"geosite-github": true,
		"geoip-cn":       true,
	}

	if len(needed) != len(expected) {
		t.Errorf("collected %d rule sets, want %d", len(needed), len(expected))
	}

	for tag := range expected {
		if !needed[tag] {
			t.Errorf("missing rule set: %s", tag)
		}
	}

	// 确保禁用的规则组没有被收集
	if needed["geosite-cn"] {
		t.Error("disabled rule group should not be collected")
	}
}

func TestRuleSetService_BuildRuleSetURL(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "ruleset-url-test")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	store, err := storage.NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	svc := NewRuleSetService(store, tmpDir)
	settings := store.GetSettings()

	// 测试无代理
	url1 := svc.buildRuleSetURL("geosite-google", settings)
	if url1 == "" {
		t.Error("URL should not be empty")
	}
	t.Logf("geosite URL (no proxy): %s", url1)

	// 测试有代理
	settings.GithubProxy = "https://ghproxy.com/"
	url2 := svc.buildRuleSetURL("geosite-google", settings)
	if url2[:len("https://ghproxy.com/")] != "https://ghproxy.com/" {
		t.Errorf("URL should start with proxy, got: %s", url2)
	}
	t.Logf("geosite URL (with proxy): %s", url2)

	// 测试 geoip
	url3 := svc.buildRuleSetURL("geoip-cn", settings)
	if url3[:len("https://ghproxy.com/")] != "https://ghproxy.com/" {
		t.Errorf("geoip URL should start with proxy, got: %s", url3)
	}
	t.Logf("geoip URL (with proxy): %s", url3)
}

func TestRuleSetService_GetLocalPath(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "ruleset-path-test")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	store, err := storage.NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	svc := NewRuleSetService(store, tmpDir)

	path := svc.GetLocalPath("geosite-google")
	expected := filepath.Join(tmpDir, "rulesets", "geosite-google.srs")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}
