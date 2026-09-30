package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

const importFixture = `{"outbounds":[{"type":"socks","tag":"SMbox/node","server":"203.0.113.8","server_port":1080}],"route":{"rules":[{"domain_suffix":["baidu.com"],"outbound":"DIRECT"},{"protocol":"bittorrent","outbound":"DIRECT"}],"final":"SMbox/node"},"inbounds":[{"type":"mixed","listen_port":12345}]}`

func TestGenericImportAndLegacyAliasesPreserveData(t *testing.T) {
	for _, base := range []string{"/api/config/import", "/api/migration"} {
		t.Run(base, func(t *testing.T) {
			s := testServer(t)
			cookie := setup(t, s)
			before := s.store.Snapshot()
			before.Settings.ImportedPolicy = &storage.ImportedPolicy{Final: "SMbox/previous", Outbounds: []map[string]any{{"type": "direct", "tag": "SMbox/previous"}}}
			if err := s.store.Replace(before); err != nil {
				t.Fatal(err)
			}
			w := request(s, "POST", base+"/preview", map[string]string{"config": importFixture}, cookie, "")
			if w.Code != 200 {
				t.Fatalf("preview %d: %s", w.Code, w.Body.String())
			}
			var report struct {
				Data struct {
					Hash     string
					Blockers []string
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Data.Blockers) > 0 {
				t.Fatal(report.Data.Blockers)
			}
			path := base + "/apply"
			if base == "/api/migration" {
				path = base + "/import"
			}
			w = request(s, "POST", path, map[string]any{"config": importFixture, "hash": report.Data.Hash, "acknowledge_omitted": true}, cookie, "")
			if w.Code != 200 {
				t.Fatalf("import %d: %s", w.Code, w.Body.String())
			}
			after := s.store.Snapshot()
			if after.Settings.ImportedPolicy.Final != "SMbox/node" || len(after.Settings.ImportedPolicy.Rules) != 2 || after.Settings.AutoApply || after.Settings.TunEnabled {
				t.Fatal("import policy or safe draft changed")
			}
			if _, err := os.Stat(filepath.Join(s.store.GetDataDir(), "generated/config.json")); !os.IsNotExist(err) {
				t.Fatal("import wrote running configuration")
			}
			w = request(s, "POST", base+"/rollback", nil, cookie, "")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			before.Settings.AutoApply = false
			if !reflect.DeepEqual(before, s.store.Snapshot()) {
				t.Fatal("old imported data or other settings lost on restore")
			}
		})
	}
}

func TestImportRejectsMissingOutboundBeforeBackup(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	config := `{"outbounds":[{"type":"socks","tag":"Proxy","server":"203.0.113.8","server_port":1080}],"route":{"final":"missing"}}`
	w := request(s, "POST", "/api/migration/preview", map[string]string{"config": config}, cookie, "")
	var report struct {
		Data struct {
			Hash     string
			Blockers []string
		}
	}
	_ = json.Unmarshal(w.Body.Bytes(), &report)
	if w.Code != 200 || len(report.Data.Blockers) == 0 {
		t.Fatalf("missing reference not shown in preview: %s", w.Body.String())
	}
	w = request(s, "POST", "/api/migration/import", map[string]any{"config": config, "hash": report.Data.Hash}, cookie, "")
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.store.GetDataDir(), "migration-before.json")); !os.IsNotExist(err) {
		t.Fatal("invalid import overwrote recovery point")
	}
}

func TestRulesOverviewSeparatesDraftFromAppliedAndSurvivesInvalidDraft(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.ImportedPolicy = &storage.ImportedPolicy{Outbounds: []map[string]any{{"type": "socks", "tag": "Proxy", "server": "203.0.113.8", "server_port": 1080, "password": "fixture-secret"}}, Final: "Proxy", Rules: []map[string]any{{"domain_suffix": []string{"baidu.com"}, "outbound": "DIRECT"}}}
	data := s.store.Snapshot()
	data.Settings = settings
	data.Rules = nil
	data.RuleGroups = nil
	if err := s.store.Replace(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.resolvePath(settings.ConfigPath), []byte(`{"route":{"rules":[{"domain_suffix":["old.example"],"outbound":"DIRECT"}],"final":"Proxy"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []bool{false, true} {
		if invalid {
			settings.ImportedPolicy.Final = "missing"
			if err := s.store.UpdateSettings(settings); err != nil {
				t.Fatal(err)
			}
		}
		w := request(s, "GET", "/api/rules/overview", nil, cookie, "")
		var response struct {
			Data struct {
				Draft, Applied *struct{ Rules []map[string]any }
				DraftError     string           `json:"draft_error"`
				ImportedRules  []map[string]any `json:"imported_rules"`
				Changed        *bool
				Running        bool
			}
		}
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		d := response.Data
		if d.Applied == nil || len(d.Applied.Rules) != 1 || len(d.ImportedRules) != 1 || d.Running {
			t.Fatal("applied/draft visibility lost", w.Body.String())
		}
		if invalid {
			if d.Draft != nil || d.DraftError == "" || d.Changed != nil {
				t.Fatal("invalid draft mislabeled")
			}
		} else if d.Draft == nil || d.Changed == nil || !*d.Changed {
			t.Fatal("draft/applied not distinguished")
		}
	}
}
