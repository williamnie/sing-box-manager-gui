package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func importedEditorFixture(t *testing.T) (*Server, *http.Cookie, string) {
	t.Helper()
	s := testServer(t)
	cookie := setup(t, s)
	d := s.store.Snapshot()
	d.Settings.AutoApply = false
	d.RuleGroups = nil
	d.Settings.ImportedPolicy = &storage.ImportedPolicy{
		SourceHash: "original-source", Final: "Proxy",
		Outbounds: []map[string]any{{"tag": "Proxy", "type": "socks", "server": "203.0.113.8", "server_port": 1080, "password": "keep-private"}},
		Rules: []map[string]any{
			{"domain_keyword": []string{"*.openai.com"}, "outbound": "Proxy", "network": []string{"tcp"}},
			{"domain": []string{"phiclouds.phicomm.com"}, "action": "reject", "source_ip_cidr": []string{"192.0.2.84/32"}, "no_drop": true},
			{"rule_set": []string{"x0"}, "outbound": "Proxy"},
		},
		RuleSets: []map[string]any{{"type": "inline", "tag": "x0", "rules": []any{map[string]any{"domain": []string{"example.com"}}}}},
	}
	if err := s.store.Replace(d); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/rules/overview", nil, cookie, "")
	var response struct {
		Data struct {
			Revision string `json:"imported_revision"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Revision == "" {
		t.Fatal("imported rules have no edit revision")
	}
	return s, cookie, response.Data.Revision
}

func TestImportedRuleEditorUpdatesOriginalRejectAndPreservesPolicy(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	before := s.store.Snapshot()
	w := request(s, "PUT", "/api/rules/imported/1", map[string]any{"revision": revision, "updates": map[string]any{"domain": []string{"phiclouds.phicomm.com", "ads.example"}}}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after := s.store.Snapshot()
	before.Settings.ImportedPolicy.Rules[1]["domain"] = []any{"phiclouds.phicomm.com", "ads.example"}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("editor changed other conditions, credentials, order or policies")
	}
	w = request(s, "PUT", "/api/rules/imported/1", map[string]any{"revision": revision, "updates": map[string]any{"domain": []string{"lost.example"}}}, cookie, "")
	if w.Code != 409 {
		t.Fatal("stale editor overwrote imported policy", w.Code)
	}
	store, err := storage.NewJSONStore(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, store.Snapshot()) {
		t.Fatal("edit was not persisted")
	}
}

func TestImportedRuleEditorRejectsUnsafeOrInvalidPatches(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	before := s.store.Snapshot()
	for _, tc := range []struct {
		index   string
		updates map[string]any
	}{
		{"1", map[string]any{"outbounds": []any{}}},
		{"1", map[string]any{"domain": []string{}, "source_ip_cidr": nil}},
		{"1", map[string]any{"domain": "ads.example"}},
		{"1", map[string]any{"domain": []string{"https://ads.example"}}},
		{"1", map[string]any{"source_ip_cidr": []string{"invalid"}}},
		{"1", map[string]any{"port": []any{0}}},
		{"1", map[string]any{"port_range": []string{"443:1"}}},
		{"1", map[string]any{"domain_regex": []string{"["}}},
		{"2", map[string]any{"rule_set": []string{"missing-set"}}},
		{"0", map[string]any{"outbound": "missing-outbound"}},
		{"-1", map[string]any{"domain": []string{"ads.example"}}},
		{"99", map[string]any{"domain": []string{"ads.example"}}},
	} {
		w := request(s, "PUT", "/api/rules/imported/"+tc.index, map[string]any{"revision": revision, "updates": tc.updates}, cookie, "")
		if w.Code < 400 {
			t.Fatalf("accepted invalid patch %+v: %s", tc, w.Body.String())
		}
		if !reflect.DeepEqual(before, s.store.Snapshot()) {
			t.Fatal("invalid request changed saved policy")
		}
	}
	if request(s, "PUT", "/api/rules/imported/1", map[string]any{}, nil, "").Code != 401 {
		t.Fatal("unauthenticated write")
	}
	if request(s, "PUT", "/api/rules/imported/1", map[string]any{}, cookie, "https://evil.example").Code != 403 {
		t.Fatal("cross origin write")
	}
}

func TestImportedRuleEditorChangesRuleSetAndActionWithoutReimport(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	w := request(s, "PUT", "/api/rules/imported/2", map[string]any{"revision": revision, "updates": map[string]any{"action": "reject", "outbound": nil, "rule_set": []string{"x0"}}}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	rule := s.store.GetSettings().ImportedPolicy.Rules[2]
	if rule["action"] != "reject" || rule["outbound"] != nil {
		t.Fatal(rule)
	}
}

type importedRuleCheckFailure struct{ fakeProcess }

func (*importedRuleCheckFailure) CheckConfig([]byte) error {
	return errors.New("fixture kernel rejected rule")
}

func TestImportedRuleEditorChecksBeforePersisting(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	s.processManager = &importedRuleCheckFailure{}
	before := s.store.Snapshot()
	w := request(s, "PUT", "/api/rules/imported/1", map[string]any{"revision": revision, "updates": map[string]any{"domain": []string{"ads.example"}}}, cookie, "")
	if w.Code != 400 || !reflect.DeepEqual(before, s.store.Snapshot()) {
		t.Fatal("kernel failure overwrote rule", w.Body.String())
	}
}

func TestRealKernelImportedRuleEditor(t *testing.T) {
	binary := os.Getenv("SBM_TEST_SINGBOX")
	if binary == "" {
		t.Skip("set SBM_TEST_SINGBOX for real kernel rule validation")
	}
	s, cookie, revision := importedEditorFixture(t)
	path := filepath.Join(s.store.GetDataDir(), "bin", "sing-box")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, path); err != nil {
		t.Fatal(err)
	}
	w := request(s, "PUT", "/api/rules/imported/1", map[string]any{"revision": revision, "updates": map[string]any{"domain": []string{"phiclouds.phicomm.com", "ads.example"}}}, cookie, "")
	var result struct{ Checked bool }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || !result.Checked {
		t.Fatal(w.Code, w.Body.String())
	}
}
