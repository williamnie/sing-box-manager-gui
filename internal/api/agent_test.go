package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/gateway"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

type agentProcess struct {
	mu            sync.Mutex
	path          string
	running       bool
	failApply     bool
	failCheck     bool
	applies       int
	starts, stops int
}

func (p *agentProcess) ApplyConfig(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failApply {
		// 真实实现失败时恢复旧文件，这里不改动文件。
		return errors.New("simulated health failure")
	}
	p.applies++
	return os.WriteFile(p.path, b, 0600)
}
func (p *agentProcess) CheckConfig([]byte) error {
	if p.failCheck {
		return errors.New("simulated kernel rejection: bad outbound")
	}
	return nil
}
func (*agentProcess) Version() (string, error) { return "sing-box version 1.14.1", nil }
func (p *agentProcess) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	p.running = true
	return nil
}
func (p *agentProcess) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stops++
	p.running = false
	return nil
}
func (p *agentProcess) Restart() error         { return nil }
func (p *agentProcess) Reload() error          { return nil }
func (p *agentProcess) IsRunning() bool        { p.mu.Lock(); defer p.mu.Unlock(); return p.running }
func (p *agentProcess) GetPID() int            { return 0 }
func (p *agentProcess) SetConfigPath(s string) { p.path = s }

type agentFixture struct {
	s      *Server
	p      *agentProcess
	cookie *http.Cookie
	token  string
}

func newAgentFixture(t *testing.T, scopes ...string) *agentFixture {
	t.Helper()
	s := testServer(t)
	p := &agentProcess{path: filepath.Join(s.store.GetDataDir(), "generated/config.json"), running: true}
	s.processManager = p
	s.fetchSubscription = func(sub *storage.Subscription) error {
		sub.Nodes = []storage.Node{{Tag: "hk-1", Type: "shadowsocks", Server: "198.51.100.10", ServerPort: 8388, Extra: map[string]any{"method": "aes-128-gcm", "password": "node-secret-pass"}}}
		sub.NodeCount = 1
		return nil
	}
	raw, err := s.buildData(s.store.Snapshot(), false)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(p.path), 0700)
	if err := os.WriteFile(p.path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cookie := setup(t, s)
	if len(scopes) == 0 {
		scopes = agentScopes
	}
	w := request(s, "POST", "/api/agent/tokens", map[string]any{"name": "test-agent", "scopes": scopes, "days": 7}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return &agentFixture{s: s, p: p, cookie: cookie, token: resp.Token}
}

func (f *agentFixture) call(method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	return agentCall(f.s, f.token, method, path, body, headers...)
}

func agentCall(s *Server, token, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	var raw []byte
	switch v := body.(type) {
	case nil:
	case string:
		raw = []byte(v)
	default:
		raw, _ = json.Marshal(v)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:9090"+path, bytes.NewReader(raw))
	req.RemoteAddr = "127.0.0.1:51234"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

type csResp struct {
	Data struct {
		ID              string   `json:"id"`
		State           string   `json:"state"`
		CandidateConfig string   `json:"candidate_config"`
		ApplyReady      bool     `json:"apply_ready"`
		Changes         []string `json:"changes"`
		ConfigChanges   []string `json:"config_changes"`
		Error           string   `json:"error"`
		Pending         *struct {
			Digest string `json:"digest"`
		} `json:"pending"`
	} `json:"data"`
	Replay bool   `json:"idempotent_replay"`
	Code   string `json:"code"`
	Error  string `json:"error"`
}

func decodeCS(t *testing.T, w *httptest.ResponseRecorder) csResp {
	t.Helper()
	var r csResp
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return r
}

func ruleOp(name, domain string) map[string]any {
	return map[string]any{"title": "test", "ops": []any{map[string]any{"action": "create", "resource": "rule", "id": name, "value": map[string]any{"name": name, "rule_type": "domain_suffix", "values": []string{domain}, "outbound": "DIRECT", "enabled": true, "priority": 5}}}}
}

func (f *agentFixture) create(t *testing.T, body any, headers ...string) csResp {
	t.Helper()
	w := f.call("POST", "/api/agent/v1/changesets", body, headers...)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return decodeCS(t, w)
}

func (f *agentFixture) apply(t *testing.T, id string, headers ...string) csResp {
	t.Helper()
	w := f.call("POST", "/api/agent/v1/changesets/"+id+"/apply", map[string]any{"confirm_seconds": 60}, headers...)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return decodeCS(t, w)
}

func appliedHas(t *testing.T, p *agentProcess, needle string) bool {
	t.Helper()
	raw, err := os.ReadFile(p.path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(raw), needle)
}

func TestAgentTokenBoundaries(t *testing.T) {
	f := newAgentFixture(t, scopeRead)
	if w := agentCall(f.s, "", "GET", "/api/agent/v1/status", nil); w.Code != 401 {
		t.Fatal("missing token accepted", w.Code)
	}
	// 管理员 Cookie 不能访问 Agent API。
	if w := request(f.s, "GET", "/api/agent/v1/status", nil, f.cookie, ""); w.Code != 401 {
		t.Fatal("cookie accepted on agent API", w.Code)
	}
	if w := f.call("GET", "/api/agent/v1/status", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.call("POST", "/api/agent/v1/changesets", ruleOp("r1", "example.com")); w.Code != 403 || !strings.Contains(w.Body.String(), "scope_denied") {
		t.Fatal("read token wrote", w.Code, w.Body.String())
	}
	// 所有旧管理路由都不接受 Bearer 令牌。
	for _, route := range f.s.router.Routes() {
		if strings.HasPrefix(route.Path, "/api/agent/v1") || !strings.HasPrefix(route.Path, "/api/") || strings.HasPrefix(route.Path, "/api/auth/") {
			continue
		}
		path := strings.NewReplacer(":id", "x", ":code", "HK", ":index", "0").Replace(route.Path)
		if w := f.call(route.Method, path, map[string]any{}); w.Code != 401 {
			t.Fatalf("legacy %s %s accepted token: %d", route.Method, route.Path, w.Code)
		}
	}
	if w := f.call("POST", "/api/auth/password", map[string]any{"current_password": "test-password-1234", "new_password": "another-password-1"}); w.Code != 401 {
		t.Fatal("token changed password", w.Code)
	}
	// CIDR 限制与吊销、过期。
	w := request(f.s, "POST", "/api/agent/tokens", map[string]any{"name": "lan-only", "scopes": []string{"read"}, "days": 1, "cidrs": []string{"192.0.2.0/24"}}, f.cookie, "")
	var created struct {
		Token string         `json:"token"`
		Data  agentTokenView `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if w := agentCall(f.s, created.Token, "GET", "/api/agent/v1/status", nil); w.Code != 403 {
		t.Fatal("cidr not enforced", w.Code)
	}
	if w := request(f.s, "DELETE", "/api/agent/tokens/"+tokenID(f.token), nil, f.cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := f.call("GET", "/api/agent/v1/status", nil); w.Code != 401 || !strings.Contains(w.Body.String(), "吊销") {
		t.Fatal("revoked token accepted", w.Code, w.Body.String())
	}
	f.s.agentTokens.mu.Lock()
	for i := range f.s.agentTokens.tokens {
		f.s.agentTokens.tokens[i].ExpiresAt = time.Now().Add(-time.Minute)
	}
	f.s.agentTokens.mu.Unlock()
	if w := agentCall(f.s, created.Token, "GET", "/api/agent/v1/status", nil); w.Code != 401 || !strings.Contains(w.Body.String(), "过期") {
		t.Fatal("expired token accepted", w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(f.s.store.GetDataDir(), "agent-tokens.json"))
	if strings.Contains(string(raw), f.token) || strings.Contains(string(raw), created.Token) {
		t.Fatal("plaintext token persisted")
	}
	if info, _ := os.Stat(filepath.Join(f.s.store.GetDataDir(), "agent-tokens.json")); info.Mode().Perm() != 0600 {
		t.Fatal("token file permissions", info.Mode())
	}
}

func tokenID(plain string) string { return strings.Split(plain, "_")[1] }

func TestAgentChangesetLifecycleAndAudit(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("corp", "corp.example"))
	if cs.Data.State != csChecked || !cs.Data.ApplyReady || len(cs.Data.ConfigChanges) == 0 {
		t.Fatalf("bad preview %+v", cs)
	}
	if appliedHas(t, f.p, "corp.example") || len(f.s.store.GetRules()) != 0 {
		t.Fatal("create mutated saved or applied state")
	}
	applied := f.apply(t, cs.Data.ID)
	if applied.Data.State != csPending || !appliedHas(t, f.p, "corp.example") {
		t.Fatalf("apply %+v", applied)
	}
	if _, err := os.Stat(f.s.agent.recoveryPath(cs.Data.ID)); err != nil {
		t.Fatal("recovery point missing during window")
	}
	if w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", map[string]any{"candidate_config": "wrong"}); w.Code != 409 {
		t.Fatal("confirm without digest", w.Code)
	}
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", map[string]any{"candidate_config": applied.Data.CandidateConfig})
	if w.Code != 200 || decodeCS(t, w).Data.State != csCommitted {
		t.Fatal(w.Code, w.Body.String())
	}
	// 已确认变更保留恢复数据，供后续撤销。
	if _, err := os.Stat(f.s.agent.recoveryPath(cs.Data.ID)); err != nil {
		t.Fatal("revert data dropped after commit")
	}
	if f.s.agentWriteBlocked() {
		t.Fatal("gate still active")
	}
	entries := f.s.auditLog.read(50)
	actions := []string{}
	for _, e := range entries {
		actions = append(actions, e.Action+":"+e.Result)
	}
	joined := strings.Join(actions, ",")
	for _, want := range []string{"changeset.create:checked", "changeset.apply:pending_confirm", "changeset.confirm:committed", "token.create:ok"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("audit missing %s in %s", want, joined)
		}
	}
}

func TestAgentIdempotency(t *testing.T) {
	f := newAgentFixture(t)
	key := []string{"Idempotency-Key", "create-key-0001"}
	a := f.create(t, ruleOp("a", "a.example"), key...)
	b := f.create(t, ruleOp("a", "a.example"), key...)
	if a.Data.ID != b.Data.ID || !b.Replay {
		t.Fatal("create retry not idempotent")
	}
	if w := f.call("POST", "/api/agent/v1/changesets", ruleOp("b", "b.example"), key...); w.Code != 422 {
		t.Fatal("same key different body accepted", w.Code)
	}
	applyKey := []string{"Idempotency-Key", "apply-key-0001"}
	first := f.apply(t, a.Data.ID, applyKey...)
	applies := f.p.applies
	second := f.apply(t, a.Data.ID, applyKey...)
	if !second.Replay || second.Data.State != first.Data.State || f.p.applies != applies {
		t.Fatal("apply retry re-executed", f.p.applies, applies)
	}
	if w := f.call("POST", "/api/agent/v1/changesets/"+a.Data.ID+"/apply", map[string]any{}, "Idempotency-Key", "apply-key-0002"); w.Code != 409 {
		t.Fatal("changeset applied twice", w.Code)
	}
}

func TestAgentDoesNotSmuggleUnappliedDraft(t *testing.T) {
	f := newAgentFixture(t)
	settings := f.s.store.GetSettings()
	settings.AutoApply = false
	if err := f.s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	// 管理界面保存但未应用的改动。
	if w := request(f.s, "POST", "/api/rules", storage.Rule{Name: "web-draft", RuleType: "domain", Values: []string{"draft.example"}, Outbound: "DIRECT", Enabled: true}, f.cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cs := f.create(t, ruleOp("agent", "agent.example"))
	if cs.Data.ApplyReady || cs.Data.Pending == nil {
		t.Fatalf("drift not reported %+v", cs.Data)
	}
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/apply", map[string]any{})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "pending_changes") {
		t.Fatal("applied with draft", w.Code, w.Body.String())
	}
	if appliedHas(t, f.p, "draft.example") || appliedHas(t, f.p, "agent.example") {
		t.Fatal("running config changed")
	}
	if w := f.call("POST", "/api/agent/v1/changesets", map[string]any{"ops": ruleOp("x", "x.example")["ops"], "include_pending": "0000"}); w.Code != 409 {
		t.Fatal("wrong include_pending accepted", w.Code)
	}
	body := ruleOp("agent", "agent.example")
	body["include_pending"] = cs.Data.Pending.Digest
	with := f.create(t, body)
	f.apply(t, with.Data.ID)
	if !appliedHas(t, f.p, "draft.example") || !appliedHas(t, f.p, "agent.example") {
		t.Fatal("explicit include_pending not applied")
	}
}

func TestAgentBaseChangedAndGate(t *testing.T) {
	f := newAgentFixture(t)
	stale := f.create(t, ruleOp("stale", "stale.example"))
	if w := request(f.s, "POST", "/api/rules", storage.Rule{Name: "web", RuleType: "domain", Values: []string{"web.example"}, Outbound: "DIRECT", Enabled: true}, f.cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := f.call("POST", "/api/agent/v1/changesets/"+stale.Data.ID+"/apply", map[string]any{}); w.Code != 409 || !strings.Contains(w.Body.String(), "base_changed") {
		t.Fatal("stale base applied", w.Code, w.Body.String())
	}
	cs := f.create(t, ruleOp("fresh", "fresh.example"))
	f.apply(t, cs.Data.ID)
	// 确认窗口内：Web 写入、运行态切换与调度被阻断，读取与认证可用。
	if w := request(f.s, "POST", "/api/rules", storage.Rule{Name: "blocked", RuleType: "domain", Values: []string{"b.example"}, Outbound: "DIRECT", Enabled: true}, f.cookie, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "operation_pending") {
		t.Fatal("web write during window", w.Code, w.Body.String())
	}
	if w := request(f.s, "POST", "/api/service/start", nil, f.cookie, ""); w.Code != 409 {
		t.Fatal("service start during window", w.Code)
	}
	if w := request(f.s, "POST", "/api/runtime/select", map[string]any{}, f.cookie, ""); w.Code != 409 {
		t.Fatal("runtime select during window", w.Code)
	}
	if w := f.call("POST", "/api/agent/v1/runtime/select", map[string]any{}); w.Code != 409 {
		t.Fatal("agent runtime select during window", w.Code)
	}
	if w := request(f.s, "GET", "/api/rules", nil, f.cookie, ""); w.Code != 200 {
		t.Fatal("read blocked", w.Code)
	}
	if !f.s.agentWriteBlocked() {
		t.Fatal("scheduler would not skip")
	}
	other := f.create(t, ruleOp("other", "other.example"))
	if w := f.call("POST", "/api/agent/v1/changesets/"+other.Data.ID+"/apply", map[string]any{}); w.Code != 409 {
		t.Fatal("second apply during window", w.Code)
	}
	if w := request(f.s, "GET", "/api/agent/state", nil, f.cookie, ""); w.Code != 200 || !strings.Contains(w.Body.String(), cs.Data.ID) {
		t.Fatal("admin state", w.Body.String())
	}
	// 管理员可在界面确认。
	if w := request(f.s, "POST", "/api/agent/changesets/"+cs.Data.ID+"/confirm", map[string]any{"candidate_config": cs.Data.CandidateConfig}, f.cookie, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(f.s, "POST", "/api/rules", storage.Rule{Name: "after", RuleType: "domain", Values: []string{"after.example"}, Outbound: "DIRECT", Enabled: true}, f.cookie, ""); w.Code != 200 {
		t.Fatal("write still blocked", w.Code)
	}
}

func TestAgentRollbackKeepsStoppedCoreStopped(t *testing.T) {
	f := newAgentFixture(t)
	f.p.running = false
	before := f.s.store.Snapshot()
	beforeCfg, _ := os.ReadFile(f.p.path)
	cs := f.create(t, ruleOp("r", "r.example"))
	f.apply(t, cs.Data.ID)
	if f.p.starts != 0 || f.p.running {
		t.Fatal("apply started stopped core")
	}
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/rollback", nil)
	if w.Code != 200 || decodeCS(t, w).Data.State != csRolledBack {
		t.Fatal(w.Code, w.Body.String())
	}
	afterCfg, _ := os.ReadFile(f.p.path)
	if storeDigest(f.s.store.Snapshot()) != storeDigest(before) || configDigest(afterCfg) != configDigest(beforeCfg) {
		t.Fatal("rollback incomplete")
	}
	if f.p.starts != 0 || f.p.running {
		t.Fatal("rollback started stopped core")
	}
	// 回滚可重试且不重复执行。
	if w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/rollback", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestAgentTimeoutRollbackAndConfirmRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		f := newAgentFixture(t)
		beforeCfg, _ := os.ReadFile(f.p.path)
		cs := f.create(t, ruleOp("race", "race.example"))
		applied := f.apply(t, cs.Data.ID)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", map[string]any{"candidate_config": applied.Data.CandidateConfig})
		}()
		f.s.agent.mu.Lock()
		f.s.scheduleTimeoutLocked(cs.Data.ID, time.Now())
		f.s.agent.mu.Unlock()
		wg.Wait()
		var state string
		for j := 0; j < 200; j++ {
			view, _ := f.s.getChangesetView(cs.Data.ID)
			state = view["state"].(string)
			if state != csPending && state != csRolling {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		cfg, _ := os.ReadFile(f.p.path)
		switch state {
		case csCommitted:
			if !strings.Contains(string(cfg), "race.example") {
				t.Fatal("committed but config reverted")
			}
		case csRolledBack:
			if configDigest(cfg) != configDigest(beforeCfg) || len(f.s.store.GetRules()) != 0 {
				t.Fatal("rolled back but state kept")
			}
		default:
			t.Fatalf("unexpected state %s", state)
		}
		if f.s.agentWriteBlocked() {
			t.Fatal("gate left active")
		}
	}
}

func TestAgentStartupRecoveryBeforeServing(t *testing.T) {
	f := newAgentFixture(t)
	beforeCfg, _ := os.ReadFile(f.p.path)
	cs := f.create(t, ruleOp("boot", "boot.example"))
	f.apply(t, cs.Data.ID)
	// 模拟管理器重启：新的 Server 读取同一数据目录，未过期的未确认操作也要先恢复。
	store, err := storage.NewJSONStore(f.s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	pm := daemon.NewProcessManager(filepath.Join(store.GetDataDir(), "bin/sing-box"), f.p.path, store.GetDataDir())
	s2 := NewServer(store, pm, nil, nil, "/test/sbm", 9090, "test")
	p2 := &agentProcess{path: f.p.path, running: true}
	s2.processManager = p2
	if !s2.agentWriteBlocked() {
		t.Fatal("pending op not gating before recovery")
	}
	s2.RecoverAgentOperations()
	view, _ := s2.getChangesetView(cs.Data.ID)
	cfg, _ := os.ReadFile(f.p.path)
	if view["state"] != csRolledBack || configDigest(cfg) != configDigest(beforeCfg) || len(store.GetRules()) != 0 || s2.agentWriteBlocked() {
		t.Fatalf("startup recovery failed: %v", view["state"])
	}
	if !p2.running {
		t.Fatal("running core stopped by recovery")
	}
}

func TestAgentApplyFailureRestoresAndRecoveryProtectsLaterData(t *testing.T) {
	f := newAgentFixture(t)
	before := f.s.store.Snapshot()
	cs := f.create(t, ruleOp("bad", "bad.example"))
	f.p.failApply = true
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/apply", map[string]any{})
	r := decodeCS(t, w)
	if w.Code != 409 || r.Data.State != csFailed || storeDigest(f.s.store.Snapshot()) != storeDigest(before) || f.s.agentWriteBlocked() {
		t.Fatalf("failed apply not restored: %d %s", w.Code, w.Body.String())
	}
	f.p.failApply = false
	cs2 := f.create(t, ruleOp("good", "good.example"))
	f.apply(t, cs2.Data.ID)
	// 绕过门禁的后续写入（例如人工编辑数据文件）不能被回滚覆盖。
	if err := f.s.store.AddRule(storage.Rule{ID: "later", Name: "later", RuleType: "domain", Values: []string{"later.example"}, Outbound: "DIRECT", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	w = f.call("POST", "/api/agent/v1/changesets/"+cs2.Data.ID+"/rollback", nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "recovery_required") {
		t.Fatal(w.Code, w.Body.String())
	}
	found := false
	for _, r := range f.s.store.GetRules() {
		found = found || r.ID == "later"
	}
	if !found || !f.s.agentWriteBlocked() {
		t.Fatal("later data overwritten or gate released")
	}
	if w := request(f.s, "POST", "/api/agent/changesets/"+cs2.Data.ID+"/resolve", map[string]any{"note": "checked by hand"}, f.cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if f.s.agentWriteBlocked() {
		t.Fatal("resolve did not release gate")
	}
}

func TestAgentRejectsGatewayNormalizedChanges(t *testing.T) {
	f := newAgentFixture(t)
	f.s.platform = "linux"
	g := &fakeGateway{}
	f.s.gateway = g
	settings := f.s.store.GetSettings()
	settings.DeploymentRole = "gateway"
	settings.Gateway = gateway.Config{Enabled: true, LANInterface: "eth0", UplinkInterface: "eth0", LANAddress: "192.0.2.2", LANCIDRs: []string{"192.0.2.0/24"}, UpstreamGateway: "192.0.2.1", IPv6Mode: "disabled"}
	settings.DeviceGroups = []storage.DeviceGroup{{ID: "nas", Name: "NAS", Policy: "bypass"}, {ID: "lan", Name: "LAN", Policy: "split"}}
	settings.Devices = []storage.Device{{ID: "box", Name: "box", Addresses: []string{"192.0.2.50"}, GroupID: "nas", Enabled: false}}
	storage.NormalizeSettings(settings)
	if err := f.s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	plan, _ := gateway.Preview("gateway", settings.Gateway)
	g.applied, g.digest = true, plan.ConfigDigest
	active, _ := json.Marshal(settings.Gateway)
	if err := os.WriteFile(filepath.Join(f.s.store.GetDataDir(), "gateway-active.json"), active, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := f.s.buildData(f.s.store.Snapshot(), false)
	_ = os.WriteFile(f.p.path, []byte(raw), 0600)
	enable := map[string]any{"ops": []any{map[string]any{"action": "update", "resource": "device", "id": "box", "value": map[string]any{"enabled": true}}}}
	if w := f.call("POST", "/api/agent/v1/changesets", enable); w.Code != 409 || !strings.Contains(w.Body.String(), "gateway_change_forbidden") {
		t.Fatal("bypass change accepted", w.Code, w.Body.String())
	}
	move := map[string]any{"ops": []any{map[string]any{"action": "create", "resource": "device", "id": "pc", "value": map[string]any{"name": "pc", "addresses": []string{"192.0.2.60"}, "group_id": "lan", "enabled": true}}}}
	cs := f.create(t, move)
	f.apply(t, cs.Data.ID)
	if g.applyCount != 0 || g.rollbackCount != 0 {
		t.Fatal("helper apply/rollback called")
	}
	if w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/rollback", nil); w.Code != 200 || g.rollbackCount != 0 {
		t.Fatal("rollback touched helper", w.Code)
	}
}

func TestAgentRedactionAndStrictFields(t *testing.T) {
	f := newAgentFixture(t)
	secretURL := "https://sub.example.net/api?token=sub-secret-token"
	body := map[string]any{"ops": []any{map[string]any{"action": "create", "resource": "subscription", "id": "sub1", "value": map[string]any{"name": "main", "url": secretURL}}}}
	w := f.call("POST", "/api/agent/v1/changesets", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cs := decodeCS(t, w)
	f.apply(t, cs.Data.ID)
	outputs := []string{w.Body.String()}
	for _, path := range []string{"/api/agent/v1/resources/subscription", "/api/agent/v1/changesets/" + cs.Data.ID, "/api/agent/v1/audit", "/api/agent/v1/status", "/api/agent/v1/nodes", "/api/agent/v1/resources/manual_node"} {
		r := f.call("GET", path, nil)
		if r.Code != 200 {
			t.Fatal(path, r.Code, r.Body.String())
		}
		outputs = append(outputs, r.Body.String())
	}
	for _, out := range outputs {
		if strings.Contains(out, "sub-secret-token") || strings.Contains(out, "node-secret-pass") {
			t.Fatalf("secret leaked: %s", out)
		}
	}
	for _, bad := range []map[string]any{
		{"ops": []any{map[string]any{"action": "update", "resource": "dns", "value": map[string]any{"mixed_port": 1}}}},
		{"ops": []any{map[string]any{"action": "update", "resource": "settings", "value": map[string]any{"auto_apply": false}}}},
		{"ops": []any{map[string]any{"action": "update", "resource": "subscription", "id": "sub1", "value": map[string]any{"url": "https://evil"}}}},
		{"ops": []any{map[string]any{"action": "create", "resource": "rule", "value": map[string]any{"name": "x", "rule_type": "process_name", "values": []string{"a"}, "outbound": "DIRECT", "extra": 1}}}},
		{"ops": []any{}},
	} {
		if r := f.call("POST", "/api/agent/v1/changesets", bad); r.Code < 400 || r.Code >= 500 {
			t.Fatalf("accepted %v: %d", bad, r.Code)
		}
	}
}

func TestAgentExplainDomainLevels(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("corp", "corp.example"))
	f.apply(t, cs.Data.ID)
	w := f.call("GET", "/api/agent/v1/explain/domain?domain=git.corp.example", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var resp struct {
		Data struct {
			Saved, Applied routeVerdict
			Runtime        struct {
				Available bool `json:"available"`
			} `json:"runtime"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Applied.Outbound != "DIRECT" || resp.Data.Applied.Decided != "rule" || resp.Data.Saved.Outbound != "DIRECT" {
		t.Fatalf("explain %s", w.Body.String())
	}
	if resp.Data.Runtime.Available {
		t.Fatal("runtime evidence claimed without kernel")
	}
}

func TestAgentKernelRejectionChangesNothing(t *testing.T) {
	f := newAgentFixture(t)
	before := f.s.store.Snapshot()
	beforeCfg, _ := os.ReadFile(f.p.path)
	f.p.failCheck = true
	w := f.call("POST", "/api/agent/v1/changesets", ruleOp("k", "k.example"))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "kernel_check_failed") {
		t.Fatal(w.Code, w.Body.String())
	}
	afterCfg, _ := os.ReadFile(f.p.path)
	if storeDigest(f.s.store.Snapshot()) != storeDigest(before) || string(afterCfg) != string(beforeCfg) || len(f.s.listChangesets(10)) != 0 {
		t.Fatal("kernel rejection left state behind")
	}
}
