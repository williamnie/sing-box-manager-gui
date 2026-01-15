package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// TestNormalizeTransportWSEarlyData 测试 WebSocket early data 规范化
func TestNormalizeTransportWSEarlyData(t *testing.T) {
	settings := storage.DefaultSettings()

	tests := []struct {
		name             string
		inputPath        string
		expectedPath     string
		expectedEarlyData int
	}{
		{
			name:             "带 ed 参数的路径",
			inputPath:        "/?ed=2048",
			expectedPath:     "/",
			expectedEarlyData: 2048,
		},
		{
			name:             "带路径和 ed 参数",
			inputPath:        "/ws?ed=4096",
			expectedPath:     "/ws",
			expectedEarlyData: 4096,
		},
		{
			name:             "无 ed 参数",
			inputPath:        "/websocket",
			expectedPath:     "/websocket",
			expectedEarlyData: 0,
		},
		{
			name:             "空路径带 ed",
			inputPath:        "?ed=2048",
			expectedPath:     "/",
			expectedEarlyData: 2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := []*storage.Node{
				{
					Tag:        "test-node",
					Type:       "vmess",
					Server:     "test.com",
					ServerPort: 443,
					Extra: map[string]interface{}{
						"uuid":     "test-uuid",
						"alter_id": 0,
						"security": "auto",
						"transport": map[string]interface{}{
							"type": "ws",
							"path": tt.inputPath,
						},
					},
				},
			}

			b := NewConfigBuilder(settings, nodes, nil, nil, nil)
			config, err := b.Build()
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			// 查找 vmess outbound
			for _, ob := range config.Outbounds {
				if ob["type"] == "vmess" {
					transport := ob["transport"].(map[string]interface{})
					
					// 检查 path
					actualPath := transport["path"].(string)
					if actualPath != tt.expectedPath {
						t.Errorf("path = %q, want %q", actualPath, tt.expectedPath)
					}

					// 检查 max_early_data
					if tt.expectedEarlyData > 0 {
						actualED, ok := transport["max_early_data"].(int)
						if !ok {
							t.Errorf("max_early_data not set, want %d", tt.expectedEarlyData)
						} else if actualED != tt.expectedEarlyData {
							t.Errorf("max_early_data = %d, want %d", actualED, tt.expectedEarlyData)
						}

						// 检查 early_data_header_name
						headerName, ok := transport["early_data_header_name"].(string)
						if !ok || headerName != "Sec-WebSocket-Protocol" {
							t.Errorf("early_data_header_name = %q, want %q", headerName, "Sec-WebSocket-Protocol")
						}
					} else {
						// 不应该有 early data 配置
						if _, ok := transport["max_early_data"]; ok {
							t.Error("max_early_data should not be set")
						}
					}
					break
				}
			}
		})
	}
}

// TestBuildRuleSetLocal 测试本地规则集生成
func TestBuildRuleSetLocal(t *testing.T) {
	settings := storage.DefaultSettings()
	ruleSetDir := filepath.Join(os.TempDir(), "test-rulesets")
	os.MkdirAll(ruleSetDir, 0755)
	defer os.RemoveAll(ruleSetDir)

	ruleGroups := []storage.RuleGroup{
		{ID: "test", Name: "测试", SiteRules: []string{"google"}, Enabled: true, Outbound: "Proxy"},
	}

	b := NewConfigBuilder(settings, nil, nil, nil, ruleGroups).WithLocalRuleSet(ruleSetDir)
	config, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// 检查规则集
	if len(config.Route.RuleSet) == 0 {
		t.Fatal("RuleSet is empty")
	}

	rs := config.Route.RuleSet[0]
	if rs.Type != "local" {
		t.Errorf("RuleSet type = %q, want %q", rs.Type, "local")
	}
	if rs.Format != "binary" {
		t.Errorf("RuleSet format = %q, want %q", rs.Format, "binary")
	}
	expectedPath := filepath.Join(ruleSetDir, "geosite-google.srs")
	if rs.Path != expectedPath {
		t.Errorf("RuleSet path = %q, want %q", rs.Path, expectedPath)
	}
	if rs.URL != "" {
		t.Error("Local RuleSet should not have URL")
	}
}

// TestBuildRuleSetRemote 测试远程规则集生成
func TestBuildRuleSetRemote(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.GithubProxy = "https://ghproxy.com/"

	ruleGroups := []storage.RuleGroup{
		{ID: "test", Name: "测试", SiteRules: []string{"google"}, IPRules: []string{"cn"}, Enabled: true, Outbound: "Proxy"},
	}

	b := NewConfigBuilder(settings, nil, nil, nil, ruleGroups)
	config, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// 检查规则集
	if len(config.Route.RuleSet) != 2 {
		t.Fatalf("RuleSet count = %d, want 2", len(config.Route.RuleSet))
	}

	// 检查 geosite 规则集
	var geositeRS, geoipRS *RuleSet
	for i := range config.Route.RuleSet {
		rs := &config.Route.RuleSet[i]
		if strings.HasPrefix(rs.Tag, "geosite-") {
			geositeRS = rs
		} else if strings.HasPrefix(rs.Tag, "geoip-") {
			geoipRS = rs
		}
	}

	if geositeRS == nil {
		t.Fatal("geosite RuleSet not found")
	}
	if geositeRS.Type != "remote" {
		t.Errorf("geosite RuleSet type = %q, want %q", geositeRS.Type, "remote")
	}
	if !strings.HasPrefix(geositeRS.URL, "https://ghproxy.com/") {
		t.Errorf("geosite RuleSet URL should have proxy prefix, got %q", geositeRS.URL)
	}
	if geositeRS.DownloadDetour != "DIRECT" {
		t.Errorf("geosite RuleSet download_detour = %q, want %q", geositeRS.DownloadDetour, "DIRECT")
	}

	if geoipRS == nil {
		t.Fatal("geoip RuleSet not found")
	}
	if geoipRS.Type != "remote" {
		t.Errorf("geoip RuleSet type = %q, want %q", geoipRS.Type, "remote")
	}
}

// TestConfigJSONOutput 测试生成的 JSON 配置是否有效
func TestConfigJSONOutput(t *testing.T) {
	settings := storage.DefaultSettings()
	
	nodes := []*storage.Node{
		{
			Tag:        "test-vmess",
			Type:       "vmess",
			Server:     "test.com",
			ServerPort: 443,
			Extra: map[string]interface{}{
				"uuid":     "test-uuid",
				"alter_id": 0,
				"security": "auto",
				"transport": map[string]interface{}{
					"type": "ws",
					"path": "/?ed=2048",
				},
				"tls": map[string]interface{}{
					"enabled":     true,
					"server_name": "test.com",
					"utls": map[string]interface{}{
						"enabled":     true,
						"fingerprint": "chrome",
					},
				},
			},
		},
	}

	ruleGroups := []storage.RuleGroup{
		{ID: "test", Name: "测试", SiteRules: []string{"google"}, Enabled: true, Outbound: "Proxy"},
	}

	ruleSetDir := filepath.Join(os.TempDir(), "test-rulesets-json")
	os.MkdirAll(ruleSetDir, 0755)
	defer os.RemoveAll(ruleSetDir)

	b := NewConfigBuilder(settings, nodes, nil, nil, ruleGroups).WithLocalRuleSet(ruleSetDir)
	jsonStr, err := b.BuildJSON()
	if err != nil {
		t.Fatalf("BuildJSON failed: %v", err)
	}

	// 验证 JSON 有效性
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	// 验证关键字段存在
	if _, ok := result["outbounds"]; !ok {
		t.Error("Missing outbounds in config")
	}
	if _, ok := result["route"]; !ok {
		t.Error("Missing route in config")
	}
	if _, ok := result["dns"]; !ok {
		t.Error("Missing dns in config")
	}

	t.Logf("Generated config length: %d bytes", len(jsonStr))
}
