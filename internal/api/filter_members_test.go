package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestFilterMembersPersistAndBuildWithSubscriptionScope(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.AutoApply = false
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []storage.Subscription{
		{ID: "a", Enabled: true, Nodes: []storage.Node{{Tag: "家宽", Type: "socks", Server: "192.0.2.1", ServerPort: 1080}}},
		{ID: "b", Enabled: true, Nodes: []storage.Node{{Tag: "家宽备用", Type: "socks", Server: "192.0.2.2", ServerPort: 1080}}},
	} {
		if err := s.store.AddSubscription(sub); err != nil {
			t.Fatal(err)
		}
	}
	w := request(s, "POST", "/api/filters", map[string]any{"name": "家庭代理", "mode": "selector", "enabled": true, "subscriptions": []string{"a"}, "node_tags": []string{"家宽", "家宽备用"}}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	filter := s.store.GetFilters()[0]
	reloaded, err := storage.NewJSONStore(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.GetFilters()[0].NodeTags, []string{"家宽", "家宽备用"}) {
		t.Fatal("members not persisted")
	}
	raw, err := s.buildData(s.store.Snapshot(), true)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range doc.Outbounds {
		if o["tag"] == "家庭代理" {
			found = true
			if !reflect.DeepEqual(o["outbounds"], []any{"家宽"}) {
				t.Fatal(o)
			}
		}
	}
	if !found {
		t.Fatal("missing family group")
	}
	filter.NodeTags = []string{}
	filter.Enabled = false
	w = request(s, "PUT", "/api/filters/"+filter.ID, filter, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reloaded, err = storage.NewJSONStore(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if tags := reloaded.GetFilters()[0].NodeTags; tags == nil || len(tags) != 0 {
		t.Fatalf("empty selection changed to dynamic: %v", tags)
	}
}

func TestSubscriptionRefreshReportsApplyFailureAsWarning(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `socks5://192.0.2.1:1080#new-node`)
	}))
	defer upstream.Close()
	for _, path := range []string{"/api/subscriptions/a/refresh", "/api/subscriptions/refresh-all"} {
		t.Run(path, func(t *testing.T) {
			s := testServer(t)
			cookie := setup(t, s)
			if err := s.store.AddSubscription(storage.Subscription{ID: "a", URL: upstream.URL, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			// 指向不存在的内核，配置保存成功但应用失败。
			settings := s.store.GetSettings()
			settings.AutoApply = true
			if err := s.store.UpdateSettings(settings); err != nil {
				t.Fatal(err)
			}
			w := request(s, "POST", path, nil, cookie, "")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["warning"] == nil {
				t.Fatalf("missing visible apply warning: %s", w.Body.String())
			}
			if got := s.store.GetSubscription("a").Nodes; len(got) != 1 || got[0].Tag != "new-node" {
				t.Fatal("subscription was not refreshed")
			}
		})
	}
}
