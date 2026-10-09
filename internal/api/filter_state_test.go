package api

import (
	"github.com/xiaobei/singbox-manager/internal/storage"
	"os"
	"strings"
	"testing"
)

func TestDisableReferencedFilterDoesNotSaveInvalidState(t *testing.T) {
	s, cookie, _ := proxyPlanFixture(t)
	if err := s.store.AddRule(storage.Rule{ID: "stun", Name: "STUN 专用", RuleType: "match", Protocol: []string{"stun"}, Outbound: "家庭代理", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	filter := s.store.GetFilters()[0]
	filter.Enabled = false
	w := request(s, "PUT", "/api/filters/"+filter.ID, filter, cookie, "")
	if w.Code != 400 {
		t.Fatalf("must reject disabled referenced group: %d %s", w.Code, w.Body.String())
	}
	if !s.store.GetFilters()[0].Enabled {
		t.Fatal("invalid disabled state was saved")
	}
	if !strings.Contains(w.Body.String(), "家庭代理") {
		t.Fatal("missing actionable reference error")
	}
}

func TestDisableUnusedFilterRemovesItFromAppliedConfig(t *testing.T) {
	s, cookie, _ := proxyPlanFixture(t)
	settings := s.store.GetSettings()
	settings.AutoApply = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	path := s.resolvePath(settings.ConfigPath)
	p := &fakeProcess{path: path, running: true}
	s.processManager = p
	filter := s.store.GetFilters()[0]
	filter.Enabled = false
	w := request(s, "PUT", "/api/filters/"+filter.ID, filter, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "家庭代理") || s.store.GetFilters()[0].Enabled {
		t.Fatal("disabled filter remained active")
	}
}

func TestFilterApplyFailureRestoresSavedState(t *testing.T) {
	s, cookie, _ := proxyPlanFixture(t)
	settings := s.store.GetSettings()
	settings.AutoApply = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	s.processManager = &proxyPlanFailProcess{fakeProcess: &fakeProcess{running: true}, failApply: true}
	filter := s.store.GetFilters()[0]
	filter.Enabled = false
	w := request(s, "PUT", "/api/filters/"+filter.ID, filter, cookie, "")
	if w.Code != 500 || !s.store.GetFilters()[0].Enabled {
		t.Fatalf("failed apply must preserve state: %d %s", w.Code, w.Body.String())
	}
}
