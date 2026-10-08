package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

const blocklistPath = "/api/rules/domain-blocklist"

func TestDomainBlocklistAppendEditAndConflict(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.AutoApply = false
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	before := s.store.Snapshot()
	w := request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "API-ACCESS.pangolin-sdk-toutiao1.com."}, cookie, "")
	if w.Code != 200 {
		t.Fatalf("append: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Rule     *storage.Rule
			Revision string
		}
		Added       bool
		Application string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Added || result.Application != "saved" || result.Data.Rule == nil {
		t.Fatal(w.Body.String())
	}
	r := result.Data.Rule
	if r.RuleType != "domain" || r.Outbound != "REJECT" || !r.Enabled || !reflect.DeepEqual(r.Values, []string{"api-access.pangolin-sdk-toutiao1.com"}) || len(r.SourceCIDRs) != 0 {
		t.Fatal(r)
	}
	oldRevision := result.Data.Revision
	w = request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": r.Values[0]}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if result.Added || len(s.store.GetRules()) != 1 {
		t.Fatal("duplicate created another rule")
	}
	w = request(s, "PUT", blocklistPath, map[string]any{"domains": []string{"ads.example", "ADS.EXAMPLE.", "track.example"}, "revision": oldRevision}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if !reflect.DeepEqual(result.Data.Rule.Values, []string{"ads.example", "track.example"}) {
		t.Fatal(result.Data.Rule)
	}
	w = request(s, "PUT", blocklistPath, map[string]any{"domains": []string{"lost.example"}, "revision": oldRevision}, cookie, "")
	if w.Code != 409 {
		t.Fatalf("stale editor overwrote list: %d", w.Code)
	}
	if !reflect.DeepEqual(s.store.GetSettings(), before.Settings) || !reflect.DeepEqual(s.store.GetRuleGroups(), before.RuleGroups) {
		t.Fatal("changed settings or upstream groups")
	}
	w = request(s, "PUT", blocklistPath, map[string]any{"domains": []string{}, "revision": result.Data.Revision}, cookie, "")
	if w.Code != 200 || len(s.store.GetRules()) != 0 {
		t.Fatalf("empty list must remove rule, not reject all: %s", w.Body.String())
	}
}

func TestDomainBlocklistBoundaryAndConcurrentAppend(t *testing.T) {
	s := testServer(t)
	if request(s, "GET", blocklistPath, nil, nil, "").Code != 401 {
		t.Fatal("read without authentication")
	}
	if request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "ads.example"}, nil, "").Code != 401 {
		t.Fatal("write without authentication")
	}
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.AutoApply = false
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"", "com", "*.example.com", "https://example.com", "192.0.2.1", "a..example", "-ads.example", "ads.example:443"} {
		w := request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": domain}, cookie, "")
		if w.Code != 400 {
			t.Fatalf("invalid domain %q: %d", domain, w.Code)
		}
	}
	if w := request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "ads.example"}, cookie, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": fmt.Sprintf("ad%d.example", i)}, cookie, "")
			if w.Code != 200 {
				t.Errorf("append: %d %s", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	rules := s.store.GetRules()
	if len(rules) != 1 || len(rules[0].Values) != 12 {
		t.Fatal("concurrent append lost entries", rules)
	}
	reloaded, err := storage.NewJSONStore(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.GetRules(), rules) {
		t.Fatal("rules not persisted")
	}
}

func TestDomainBlocklistDisabledAndFailedApply(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	// 测试内核不存在：保存必须保留并报告应用失败，不能声称已拦截。
	w := request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "ads.example"}, cookie, "")
	var response struct {
		Application string
		Warning     string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || response.Application != "failed" || response.Warning == "" || len(s.store.GetRules()) != 1 {
		t.Fatal(w.Body.String())
	}
	rule := s.store.GetRules()[0]
	rule.Enabled = false
	if err := s.store.UpdateRule(rule); err != nil {
		t.Fatal(err)
	}
	w = request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "another.example"}, cookie, "")
	if w.Code != 409 || !reflect.DeepEqual(s.store.GetRules()[0], rule) {
		t.Fatal("append unexpectedly enabled existing blocked domains")
	}
}

func TestDomainBlocklistDoesNotBroadenEditedRule(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.AutoApply = false
	_ = s.store.UpdateSettings(settings)
	w := request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "ads.example"}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	r := s.store.GetRules()[0]
	r.SourceCIDRs = []string{"192.0.2.10/32"}
	_ = s.store.UpdateRule(r)
	w = request(s, "POST", blocklistPath+"/domains", map[string]string{"domain": "other.example"}, cookie, "")
	if w.Code != 409 || !reflect.DeepEqual(s.store.GetRules()[0], r) {
		t.Fatal("silently broadened an edited rule")
	}
}
