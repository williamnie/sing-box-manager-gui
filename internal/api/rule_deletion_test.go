package api

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestImportedRuleDeletionPreservesPolicyAndPersists(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	before := s.store.Snapshot()
	w := request(s, "DELETE", "/api/rules/imported/1", map[string]any{"revision": revision}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := s.store.Snapshot()
	before.Settings.ImportedPolicy.Rules = append(before.Settings.ImportedPolicy.Rules[:1], before.Settings.ImportedPolicy.Rules[2:]...)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("deletion changed other rules, their order, credentials or final outbound")
	}
	store, err := storage.NewJSONStore(s.store.GetDataDir())
	if err != nil || !reflect.DeepEqual(after, store.Snapshot()) {
		t.Fatal("deletion was not persisted", err)
	}
	// 删除会使后续行前移，旧版本不能继续删除或编辑同一个位置。
	for _, method := range []string{"DELETE", "PUT"} {
		w = request(s, method, "/api/rules/imported/1", map[string]any{"revision": revision, "updates": map[string]any{"outbound": "Proxy"}}, cookie, "")
		if w.Code != 409 || !reflect.DeepEqual(after, s.store.Snapshot()) {
			t.Fatal("stale request changed the shifted rule", method, w.Code, w.Body.String())
		}
	}
}

func TestImportedRuleDeletionRejectsInvalidRequests(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	before := s.store.Snapshot()
	for _, tc := range []struct {
		index string
		body  any
		code  int
	}{
		{"-1", map[string]any{"revision": revision}, 400},
		{"invalid", map[string]any{"revision": revision}, 400},
		{"99", map[string]any{"revision": revision}, 404},
		{"1", nil, 400},
		{"1", map[string]any{}, 400},
		{"1", map[string]any{"revision": "stale"}, 409},
	} {
		w := request(s, "DELETE", "/api/rules/imported/"+tc.index, tc.body, cookie, "")
		if w.Code != tc.code || !reflect.DeepEqual(before, s.store.Snapshot()) {
			t.Fatal(tc.index, w.Code, w.Body.String())
		}
	}
	if request(s, "DELETE", "/api/rules/imported/1", map[string]any{"revision": revision}, nil, "").Code != 401 {
		t.Fatal("unauthenticated deletion allowed")
	}
	if request(s, "DELETE", "/api/rules/imported/1", map[string]any{"revision": revision}, cookie, "https://evil.example").Code != 403 {
		t.Fatal("cross origin deletion allowed")
	}
	s.processManager = &importedRuleCheckFailure{}
	w := request(s, "DELETE", "/api/rules/imported/1", map[string]any{"revision": revision}, cookie, "")
	if w.Code != 400 || !reflect.DeepEqual(before, s.store.Snapshot()) {
		t.Fatal("kernel check failure removed saved rule", w.Code, w.Body.String())
	}
}

type ruleDeletionApplyFailure struct{ fakeProcess }

func (p *ruleDeletionApplyFailure) ApplyConfig([]byte) error {
	p.applies++
	return errors.New("fixture apply failed; previous process retained")
}

func TestRuleDeletionApplicationModes(t *testing.T) {
	for _, kind := range []string{"imported", "custom"} {
		for _, tc := range []struct {
			name        string
			autoApply   bool
			running     bool
			fail        bool
			application string
			applies     int
		}{
			{"manual", false, true, false, "saved", 0},
			{"running", true, true, false, "applied", 1},
			{"stopped", true, false, false, "saved", 1},
			{"apply failure", true, true, true, "failed", 1},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				s, cookie, revision := importedEditorFixture(t)
				settings := s.store.GetSettings()
				settings.AutoApply = tc.autoApply
				if err := s.store.UpdateSettings(settings); err != nil {
					t.Fatal(err)
				}
				p := &fakeProcess{running: tc.running, path: filepath.Join(s.store.GetDataDir(), "generated/config.json")}
				s.processManager = p
				if tc.fail {
					failed := &ruleDeletionApplyFailure{fakeProcess: *p}
					s.processManager, p = failed, &failed.fakeProcess
				}
				path := "/api/rules/imported/1"
				if kind == "custom" {
					if err := s.store.AddRule(storage.Rule{ID: "delete-me", Name: "删除测试", RuleType: "domain", Values: []string{"ads.example"}, Outbound: "REJECT", Enabled: true}); err != nil {
						t.Fatal(err)
					}
					path = "/api/rules/delete-me"
				}
				w := request(s, "DELETE", path, map[string]any{"revision": revision}, cookie, "")
				var result struct{ Application, Warning string }
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || result.Application != tc.application || p.applies != tc.applies || p.running != tc.running || (result.Warning != "") != tc.fail {
					t.Fatal(w.Code, w.Body.String(), p)
				}
				if kind == "imported" && len(s.store.GetSettings().ImportedPolicy.Rules) != 2 {
					t.Fatal("imported deletion was not saved")
				}
				if kind == "custom" && len(s.store.GetRules()) != 0 {
					t.Fatal("custom deletion was not saved")
				}
			})
		}
	}
}

func TestImportedRuleDeletionAllowsEmptyRuleList(t *testing.T) {
	s, cookie, _ := importedEditorFixture(t)
	settings := s.store.GetSettings()
	settings.ImportedPolicy.Rules = settings.ImportedPolicy.Rules[:1]
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	w := request(s, "DELETE", "/api/rules/imported/0", map[string]any{"revision": importedPolicyRevision(settings.ImportedPolicy)}, cookie, "")
	if w.Code != 200 || len(s.store.GetSettings().ImportedPolicy.Rules) != 0 || s.store.GetSettings().ImportedPolicy.Final != "Proxy" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestRuleDeletionDoesNotActivateGateway(t *testing.T) {
	s, p, g := prepareGateway(t)
	cookie := setup(t, s)
	data := s.store.Snapshot()
	data.RuleGroups = nil // 与完整配置导入后的状态一致。
	settings := data.Settings
	settings.AutoApply = true
	settings.ImportedPolicy = &storage.ImportedPolicy{
		Final: "Local Direct", Outbounds: []map[string]any{{"tag": "Local Direct", "type": "direct"}},
		Rules: []map[string]any{{"domain": []string{"ads.example"}, "action": "reject"}},
	}
	if err := s.store.Replace(data); err != nil {
		t.Fatal(err)
	}
	p.running = true
	w := request(s, "DELETE", "/api/rules/imported/0", map[string]any{"revision": importedPolicyRevision(settings.ImportedPolicy)}, cookie, "")
	var result struct{ Application string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Application != "saved" || p.applies != 0 || g.applyCount != 0 || s.store.GetSettings().Gateway.Enabled {
		t.Fatal("deletion activated gateway topology", w.Code, w.Body.String(), p, g)
	}
}
