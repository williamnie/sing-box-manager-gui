package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// restartServer 模拟管理器重启：新的 Server 读取同一数据目录并注入受控的内核与启动标识。
func restartServer(t *testing.T, f *agentFixture, boot string, running bool) (*Server, *agentProcess) {
	t.Helper()
	store, err := storage.NewJSONStore(f.s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	pm := daemon.NewProcessManager(filepath.Join(store.GetDataDir(), "bin/sing-box"), f.p.path, store.GetDataDir())
	s2 := NewServer(store, pm, nil, nil, "/test/sbm", 9090, "test")
	p2 := &agentProcess{path: f.p.path, running: running}
	s2.processManager = p2
	s2.bootID = func() string { return boot }
	return s2, p2
}

func TestAgentRecoveryRespectsBootBoundary(t *testing.T) {
	cases := []struct {
		name          string
		boot          string
		runningAfter  bool
		wantState     string
		wantStarts    int
		wantCandidate bool
	}{
		{"manager restart restarts core", "boot-A", false, csRolledBack, 1, false},
		{"host reboot never starts core", "boot-B", false, csRolledBack, 0, false},
		{"host reboot with running core keeps scene", "boot-B", true, csRecovery, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAgentFixture(t)
			f.s.bootID = func() string { return "boot-A" }
			cs := f.create(t, ruleOp("boot", "boot.example"))
			f.apply(t, cs.Data.ID)
			s2, p2 := restartServer(t, f, tc.boot, tc.runningAfter)
			s2.RecoverAgentOperations()
			view, _ := s2.getChangesetView(cs.Data.ID)
			if view["state"] != tc.wantState || p2.starts != tc.wantStarts || appliedHas(t, f.p, "boot.example") != tc.wantCandidate {
				t.Fatalf("state=%v starts=%d candidate=%v resolution=%v error=%v", view["state"], p2.starts, appliedHas(t, f.p, "boot.example"), view["resolution"], view["error"])
			}
			if tc.wantState == csRecovery && !s2.agentWriteBlocked() {
				t.Fatal("recovery_required did not keep the gate")
			}
			if tc.boot == "boot-B" && tc.wantState == csRolledBack && !strings.Contains(view["resolution"].(string), "未自动启动") {
				t.Fatal("reboot note missing", view["resolution"])
			}
		})
	}
}

func TestAgentTerminalPersistFailureKeepsRecoveryAndGate(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("p", "p.example"))
	f.apply(t, cs.Data.ID)
	f.s.agent.mu.Lock()
	f.s.agent.persistHook = func(c *changeset) error {
		if c.State == csRolledBack || c.State == csRecovery {
			return errors.New("disk full")
		}
		return nil
	}
	f.s.agent.mu.Unlock()
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/rollback", nil)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "journal_write_failed") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(f.s.agent.recoveryPath(cs.Data.ID)); err != nil {
		t.Fatal("recovery point dropped before terminal state persisted")
	}
	if !f.s.agentWriteBlocked() {
		t.Fatal("gate released after failed persist")
	}

	// 应用失败的终态同样先落盘再清理。
	g := newAgentFixture(t)
	cs2 := g.create(t, ruleOp("q", "q.example"))
	g.p.failApply = true
	g.s.agent.persistHook = func(c *changeset) error {
		if c.State == csFailed || c.State == csRecovery {
			return errors.New("disk full")
		}
		return nil
	}
	w = g.call("POST", "/api/agent/v1/changesets/"+cs2.Data.ID+"/apply", map[string]any{})
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(g.s.agent.recoveryPath(cs2.Data.ID)); err != nil || !g.s.agentWriteBlocked() {
		t.Fatal("apply failure lost recovery point or gate")
	}
}

func TestAgentCorruptJournalBlocksAgentNamespace(t *testing.T) {
	f := newAgentFixture(t)
	valid := f.create(t, ruleOp("v", "v.example"))
	if err := os.WriteFile(filepath.Join(f.s.agent.dir, "broken.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	s2, _ := restartServer(t, f, "boot-A", true)
	if w := agentCall(s2, f.token, "POST", "/api/agent/v1/changesets/"+valid.Data.ID+"/apply", map[string]any{}); w.Code != 503 || !strings.Contains(w.Body.String(), "journal_unavailable") {
		t.Fatal("apply allowed with corrupt journal", w.Code, w.Body.String())
	}
	if w := agentCall(s2, f.token, "POST", "/api/agent/v1/changesets", ruleOp("n", "n.example")); w.Code != 503 {
		t.Fatal("create allowed with corrupt journal", w.Code)
	}
	if w := agentCall(s2, f.token, "GET", "/api/agent/v1/status", nil); w.Code != 200 {
		t.Fatal("reads should still work", w.Code)
	}
}

func TestAgentConfirmAfterDeadlineRejected(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("d", "d.example"))
	applied := f.apply(t, cs.Data.ID)
	f.s.agent.mu.Lock()
	f.s.stopTimerLocked(cs.Data.ID)
	past := time.Now().Add(-time.Second)
	f.s.agent.sets[cs.Data.ID].Deadline = &past
	f.s.agent.mu.Unlock()
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", map[string]any{"candidate_config": applied.Data.CandidateConfig})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "deadline_passed") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestAgentApplyIdempotencyBindsConfirmWindow(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("i", "i.example"))
	key := []string{"Idempotency-Key", "apply-key-bind-01"}
	if w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/apply", map[string]any{"confirm_seconds": 60}, key...); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/apply", map[string]any{"confirm_seconds": 90}, key...); w.Code != 422 {
		t.Fatal("same key different confirm window accepted", w.Code)
	}
}

// 在 -race 下运行：幂等重放与状态修改并发时不能产生数据竞争。
func TestAgentCreateReplayConcurrentWithApply(t *testing.T) {
	f := newAgentFixture(t)
	key := []string{"Idempotency-Key", "race-create-key-1"}
	cs := f.create(t, ruleOp("r", "r.example"), key...)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.call("POST", "/api/agent/v1/changesets", ruleOp("r", "r.example"), key...)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/apply", map[string]any{})
	}()
	wg.Wait()
}

func TestAgentRollbackKeepsSceneOnRuntimeDrift(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("rd", "rd.example"))
	f.apply(t, cs.Data.ID)
	beforeRev := storeDigest(f.s.store.Snapshot())
	if err := os.WriteFile(f.p.path, []byte(`{"log":{"level":"warn"},"edited":"by hand"}`), 0600); err != nil {
		t.Fatal(err)
	}
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/rollback", nil)
	raw, _ := os.ReadFile(f.p.path)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "recovery_required") || !strings.Contains(string(raw), "by hand") {
		t.Fatal("runtime drift overwritten", w.Code, w.Body.String())
	}
	if storeDigest(f.s.store.Snapshot()) != beforeRev {
		t.Fatal("saved configuration changed before runtime drift was rejected")
	}
}

func TestAgentExpiredChangesetsArePrunedAndCapped(t *testing.T) {
	f := newAgentFixture(t)
	old := f.create(t, ruleOp("old", "old.example"))
	f.s.agent.mu.Lock()
	f.s.agent.sets[old.Data.ID].ExpiresAt = time.Now().Add(-time.Minute)
	f.s.agent.mu.Unlock()
	f.create(t, ruleOp("new", "new.example"))
	view, _ := f.s.getChangesetView(old.Data.ID)
	if view["state"] != csExpired {
		t.Fatal("stale checked changeset not expired", view["state"])
	}
	raw, _ := os.ReadFile(f.s.agent.path(old.Data.ID))
	if !strings.Contains(string(raw), `"state": "expired"`) {
		t.Fatal("expired state not persisted")
	}
	// 填满未过期的待应用变更集后拒绝新建，而不是删除活动记录。
	f.s.agent.mu.Lock()
	// 多填一条，抵消 prune 删除的那条已过期记录。
	for i := len(f.s.agent.sets); i <= maxChangesetFiles; i++ {
		id := "filler-" + time.Now().Add(time.Duration(i)).Format("150405.000000000")
		f.s.agent.sets[id] = &changeset{ID: id, State: csChecked, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	}
	f.s.agent.mu.Unlock()
	if w := f.call("POST", "/api/agent/v1/changesets", ruleOp("over", "over.example")); w.Code != 429 {
		t.Fatal("cap not enforced", w.Code)
	}
}

func TestAgentImportedRuleOps(t *testing.T) {
	s, cookie, revision := importedEditorFixture(t)
	f := &agentFixture{s: s, cookie: cookie}
	p := &agentProcess{path: filepath.Join(s.store.GetDataDir(), "generated/config.json")}
	s.processManager = p
	raw, _ := s.buildData(s.store.Snapshot(), false)
	_ = os.MkdirAll(filepath.Dir(p.path), 0700)
	_ = os.WriteFile(p.path, []byte(raw), 0600)
	f.p = p
	w := request(s, "POST", "/api/agent/tokens", map[string]any{"name": "imp", "scopes": agentScopes, "days": 1}, cookie, "")
	var created struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	f.token = created.Token

	if r := f.call("GET", "/api/agent/v1/resources/imported_rule", nil); r.Code != 200 || !strings.Contains(r.Body.String(), revision) || strings.Contains(r.Body.String(), "keep-private") {
		t.Fatal("imported list", r.Code, r.Body.String())
	}
	stale := map[string]any{"ops": []any{map[string]any{"action": "update", "resource": "imported_rule", "id": "0", "value": map[string]any{"revision": "stale", "updates": map[string]any{"outbound": "DIRECT"}}}}}
	if r := f.call("POST", "/api/agent/v1/changesets", stale); r.Code != 409 || !strings.Contains(r.Body.String(), "imported_revision_changed") {
		t.Fatal("stale revision accepted", r.Code, r.Body.String())
	}
	update := map[string]any{"ops": []any{map[string]any{"action": "update", "resource": "imported_rule", "id": "1", "value": map[string]any{"revision": revision, "updates": map[string]any{"domain": []string{"phiclouds.phicomm.com", "ads.example"}}}}}}
	cs := f.create(t, update)
	f.apply(t, cs.Data.ID)
	rule := s.store.GetSettings().ImportedPolicy.Rules[1]
	if rule["no_drop"] != true || rule["action"] != "reject" || len(rule["domain"].([]any)) != 2 {
		t.Fatalf("update lost extension fields: %v", rule)
	}
	unsupported := map[string]any{"ops": []any{map[string]any{"action": "update", "resource": "imported_rule", "id": "0", "value": map[string]any{"revision": revision, "updates": map[string]any{"override_address": "1.1.1.1"}}}}}
	if r := f.call("POST", "/api/agent/v1/changesets", unsupported); r.Code < 400 {
		t.Fatal("unsupported imported field accepted")
	}
}

func TestAgentRevertCommittedChange(t *testing.T) {
	f := newAgentFixture(t)
	beforeCfg, _ := os.ReadFile(f.p.path)
	cs := f.create(t, ruleOp("keep", "keep.example"))
	applied := f.apply(t, cs.Data.ID)
	f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", map[string]any{"candidate_config": applied.Data.CandidateConfig})
	if w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/rollback", nil); w.Code != 409 {
		t.Fatal("committed change rolled back directly", w.Code)
	}
	revert := f.create(t, map[string]any{"ops": []any{map[string]any{"action": "apply", "resource": "revert", "id": cs.Data.ID}}})
	if len(revert.Data.ConfigChanges) == 0 || appliedHas(t, f.p, "keep.example") == false {
		t.Fatal("revert preview should not modify, but should show diff")
	}
	ra := f.apply(t, revert.Data.ID)
	f.call("POST", "/api/agent/v1/changesets/"+revert.Data.ID+"/confirm", map[string]any{"candidate_config": ra.Data.CandidateConfig})
	cfg, _ := os.ReadFile(f.p.path)
	if configDigest(cfg) != configDigest(beforeCfg) || len(f.s.store.GetRules()) != 0 {
		t.Fatal("revert did not restore")
	}

	// 之后有其他修改时拒绝撤销，不覆盖后续数据。
	cs2 := f.create(t, ruleOp("a", "a.example"))
	a2 := f.apply(t, cs2.Data.ID)
	f.call("POST", "/api/agent/v1/changesets/"+cs2.Data.ID+"/confirm", map[string]any{"candidate_config": a2.Data.CandidateConfig})
	cs3 := f.create(t, ruleOp("b", "b.example"))
	a3 := f.apply(t, cs3.Data.ID)
	f.call("POST", "/api/agent/v1/changesets/"+cs3.Data.ID+"/confirm", map[string]any{"candidate_config": a3.Data.CandidateConfig})
	w := f.call("POST", "/api/agent/v1/changesets", map[string]any{"ops": []any{map[string]any{"action": "apply", "resource": "revert", "id": cs2.Data.ID}}})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "revert_conflict") {
		t.Fatal("revert over later change", w.Code, w.Body.String())
	}
	// 管理员界面路径。
	if w := request(f.s, "POST", "/api/agent/changesets/"+cs3.Data.ID+"/revert", map[string]any{}, f.cookie, ""); w.Code != 200 {
		t.Fatal("admin revert", w.Code, w.Body.String())
	}
}

// 确认记录写失败后，原有确认期限仍须有效；不能因此无限延长确认窗口。
func TestAgentConfirmPersistFailurePreservesDeadline(t *testing.T) {
	f := newAgentFixture(t)
	cs := f.create(t, ruleOp("deadline-persist", "deadline-persist.example"))
	applied := f.apply(t, cs.Data.ID)
	deadline := time.Now().Add(150 * time.Millisecond)
	f.s.agent.mu.Lock()
	f.s.stopTimerLocked(cs.Data.ID)
	f.s.agent.sets[cs.Data.ID].Deadline = &deadline
	f.s.agent.persistHook = func(c *changeset) error {
		if c.State == csCommitted {
			return errors.New("confirmation persistence failed")
		}
		return nil
	}
	f.s.agent.mu.Unlock()
	body := map[string]any{"candidate_config": applied.Data.CandidateConfig}
	w := f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", body)
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.s.agent.mu.Lock()
	retained := f.s.agent.sets[cs.Data.ID].Deadline
	f.s.agent.persistHook = nil
	f.s.agent.mu.Unlock()
	if retained == nil || !retained.Equal(deadline) {
		t.Fatal("confirmation persistence failure lost the original deadline")
	}
	time.Sleep(time.Until(deadline) + 20*time.Millisecond)
	w = f.call("POST", "/api/agent/v1/changesets/"+cs.Data.ID+"/confirm", body)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "deadline_passed") {
		t.Fatal("late confirmation accepted", w.Code, w.Body.String())
	}
}
