package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func proxyPlanFixture(t *testing.T) (*Server, *http.Cookie, *storage.ProxyPlan) {
	t.Helper()
	s := testServer(t)
	cookie := setup(t, s)
	data := s.store.Snapshot()
	data.Settings.AutoApply = false
	data.Settings.ImportedPolicy = &storage.ImportedPolicy{Final: "旧默认", Outbounds: []map[string]any{
		{"type": "socks", "tag": "旧节点", "server": "192.0.2.1", "server_port": 1080, "password": "private-fixture-password"},
		{"type": "selector", "tag": "旧默认", "outbounds": []string{"旧节点"}},
		{"type": "selector", "tag": "Proxy", "outbounds": []string{"旧默认"}},
	}, Rules: []map[string]any{{"domain_suffix": []string{"example.com"}, "outbound": "旧默认"}}}
	data.Filters = []storage.Filter{{ID: "family", Name: "家庭代理", Mode: "selector", Enabled: true, AllNodes: true}}
	data.Subscriptions = []storage.Subscription{{ID: "sub", Enabled: true, Nodes: []storage.Node{{Tag: "新节点", Type: "socks", Server: "192.0.2.2", ServerPort: 1080}}}}
	if err := s.store.Replace(data); err != nil {
		t.Fatal(err)
	}
	return s, cookie, &storage.ProxyPlan{Primary: "家庭代理", MergeGroups: []string{"旧默认", "Proxy"}}
}
func previewPlan(t *testing.T, s *Server, cookie *http.Cookie, plan *storage.ProxyPlan) string {
	t.Helper()
	w := request(s, "POST", "/api/proxy-plan/preview", proxyPlanRequest{Plan: plan}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Revision string           `json:"revision"`
			After    proxyPlanSummary `json:"after"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "private-fixture-password") {
		t.Fatal("preview leaked credentials")
	}
	if plan != nil && (response.Data.After.Final != "家庭代理" || !reflect.DeepEqual(response.Data.After.Groups, []string{"家庭代理"})) {
		t.Fatal(response.Data.After)
	}
	return response.Data.Revision
}
func TestProxyPlanPreviewSaveRestoreAndStaleDraft(t *testing.T) {
	s, cookie, plan := proxyPlanFixture(t)
	original, _ := json.Marshal(s.store.Snapshot())
	revision := previewPlan(t, s, cookie, plan)
	after, _ := json.Marshal(s.store.Snapshot())
	if string(after) != string(original) {
		t.Fatal("preview mutated store")
	}
	changed := *plan
	changed.MergeGroups = []string{"旧默认"}
	if w := request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: &changed, Revision: revision}, cookie, ""); w.Code != 409 {
		t.Fatal("changed plan used old preview", w.Code)
	}
	if err := s.store.AddManualNode(storage.ManualNode{ID: "extra", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if w := request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: plan, Revision: revision}, cookie, ""); w.Code != 409 {
		t.Fatal("stale draft accepted", w.Code)
	}
	revision = previewPlan(t, s, cookie, plan)
	policy := s.store.GetSettings().ImportedPolicy
	w := request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: plan, Revision: revision}, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"saved"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	overview := request(s, "GET", "/api/rules/overview", nil, cookie, "")
	if overview.Code != 200 {
		t.Fatal(overview.Body.String())
	}
	var visible struct {
		Data struct {
			Redirects map[string]string `json:"outbound_redirects"`
			Rules     []map[string]any  `json:"imported_rules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(overview.Body.Bytes(), &visible); err != nil {
		t.Fatal(err)
	}
	if visible.Data.Redirects["旧默认"] != "家庭代理" || visible.Data.Rules[0]["outbound"] != "旧默认" {
		t.Fatal("effective display or original edit target lost", visible)
	}
	reloaded, err := storage.NewJSONStore(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.GetSettings().ProxyPlan, plan) || !reflect.DeepEqual(reloaded.GetSettings().ImportedPolicy, policy) {
		t.Fatal("plan or source was not preserved")
	}
	revision = previewPlan(t, s, cookie, nil)
	w = request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Revision: revision}, cookie, "")
	if w.Code != 200 || s.store.GetSettings().ProxyPlan != nil || !reflect.DeepEqual(s.store.GetSettings().ImportedPolicy, policy) {
		t.Fatal("restore failed", w.Body.String())
	}
}

type proxyPlanFailProcess struct {
	*fakeProcess
	failCheck, failApply bool
}

func (p *proxyPlanFailProcess) CheckConfig([]byte) error {
	if p.failCheck {
		return errors.New("check fixture failure")
	}
	return nil
}
func (p *proxyPlanFailProcess) ApplyConfig([]byte) error {
	if p.failApply {
		return errors.New("apply fixture failure")
	}
	return nil
}
func TestProxyPlanCheckFailureDoesNotSaveAndApplyFailureIsVisible(t *testing.T) {
	s, cookie, plan := proxyPlanFixture(t)
	p := &proxyPlanFailProcess{fakeProcess: &fakeProcess{running: true}, failCheck: true}
	s.processManager = p
	revision := previewPlan(t, s, cookie, plan)
	w := request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: plan, Revision: revision}, cookie, "")
	if w.Code != 400 || s.store.GetSettings().ProxyPlan != nil {
		t.Fatal("failed check saved plan", w.Body.String())
	}
	settings := s.store.GetSettings()
	settings.AutoApply = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	p.failCheck = false
	p.failApply = true
	revision = previewPlan(t, s, cookie, plan)
	w = request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: plan, Revision: revision}, cookie, "")
	if w.Code != 500 || s.store.GetSettings().ProxyPlan != nil || !strings.Contains(w.Body.String(), "已恢复") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestProxyPlanRequiresAuthenticationAndPreview(t *testing.T) {
	s, cookie, plan := proxyPlanFixture(t)
	for _, path := range []string{"/api/proxy-plan/preview", "/api/proxy-plan"} {
		method := "POST"
		if path == "/api/proxy-plan" {
			method = "PUT"
		}
		if w := request(s, method, path, proxyPlanRequest{Plan: plan}, nil, ""); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
		if w := request(s, method, path, proxyPlanRequest{Plan: plan}, cookie, "https://evil.example"); w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	if w := request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: plan}, cookie, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestRuntimeProxyRouteUsesAppliedConfig(t *testing.T) {
	s := testServer(t)
	settings := s.store.GetSettings()
	settings.FinalOutbound = "draft-only"
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.resolvePath(settings.ConfigPath), []byte(`{"route":{"final":"applied-group"},"outbounds":[{"password":"secret"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := proxySnapshot{}
	s.annotateProxyRoute(&snapshot)
	raw, _ := json.Marshal(snapshot)
	if snapshot.RouteFinal != "applied-group" || strings.Contains(string(raw), "secret") {
		t.Fatal(string(raw))
	}
}

func TestManagedSourceMigrationRepairsDisabledFilterAndBacksUpData(t *testing.T) {
	s, cookie, _ := proxyPlanFixture(t)
	data := s.store.Snapshot()
	data.Filters[0].Enabled = false
	data.Rules = []storage.Rule{{ID: "stun", Name: "STUN", RuleType: "match", Protocol: []string{"stun"}, Outbound: "家庭代理", Enabled: true}}
	data.Settings.ImportedPolicy.Outbounds = append(data.Settings.ImportedPolicy.Outbounds, map[string]any{"type": "socks", "tag": "SMbox/专用", "server": "192.0.2.10", "server_port": 1080})
	data.Settings.ImportedPolicy.Rules = append(data.Settings.ImportedPolicy.Rules, map[string]any{"domain": []string{"special.example"}, "outbound": "SMbox/专用"})
	if err := s.store.Replace(data); err != nil {
		t.Fatal(err)
	}
	plan := &storage.ProxyPlan{Primary: "Proxy", ManagedOnly: true, DefaultNode: "新节点"}
	w := request(s, "POST", "/api/proxy-plan/preview", proxyPlanRequest{Plan: plan}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Data struct {
			Revision string           `json:"revision"`
			After    proxyPlanSummary `json:"after"`
			Adopted  []string         `json:"adopted_nodes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Data.After.Groups, []string{"Proxy"}) || !reflect.DeepEqual(response.Data.Adopted, []string{"专用"}) {
		t.Fatal(response.Data)
	}
	if len(s.store.GetSettings().ImportedPolicy.Outbounds) == 0 {
		t.Fatal("preview mutated stored data")
	}
	w = request(s, "PUT", "/api/proxy-plan", proxyPlanRequest{Plan: plan, Revision: response.Data.Revision}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved := s.store.Snapshot()
	if !saved.Settings.ProxyPlan.ManagedOnly || len(saved.Settings.ImportedPolicy.Outbounds) != 0 || len(saved.ManualNodes) != 1 || saved.Filters[0].Enabled || saved.Rules[0].Outbound != "Proxy" {
		t.Fatal("source migration incomplete")
	}
	backups, err := filepath.Glob(filepath.Join(s.store.GetDataDir(), "managed-nodes-before-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatal("missing backup")
	}
	raw, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	var backup storage.AppData
	if err := json.Unmarshal(raw, &backup); err != nil {
		t.Fatal(err)
	}
	if backup.Settings.ProxyPlan != nil || len(backup.Settings.ImportedPolicy.Outbounds) != 4 || backup.Filters[0].Enabled {
		t.Fatal("backup not original data")
	}
	stat, err := os.Stat(backups[0])
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("unsafe backup permissions")
	}
}
