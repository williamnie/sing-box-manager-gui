package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFastCloneIsolatesStoreAndRollsBack(t *testing.T) {
	s, err := NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	extra := map[string]any{"tls": map[string]any{"server_name": "a"}, "alpn": []any{"h2"}}
	sub := Subscription{ID: "s", Nodes: []Node{{Tag: "n", SubscriptionID: "s", Extra: extra}}}
	if err := s.AddSubscription(sub); err != nil {
		t.Fatal(err)
	}
	// 调用方之后修改入参或读取结果，都不能影响存储。
	extra["tls"].(map[string]any)["server_name"] = "changed"
	got := s.GetSubscriptions()
	got[0].Nodes[0].Extra["alpn"].([]any)[0] = "changed"
	snapshot := s.Snapshot()
	snapshot.Subscriptions[0].Nodes[0].Tag = "changed"
	node := s.GetSubscription("s").Nodes[0]
	if node.Tag != "n" || node.SubscriptionID != "" || node.Extra["tls"].(map[string]any)["server_name"] != "a" || node.Extra["alpn"].([]any)[0] != "h2" {
		t.Fatalf("store mutated through alias: %+v", node)
	}
	// 写入失败时回滚到最近一次成功落盘的数据。
	path := filepath.Join(s.GetDataDir(), "data.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if missing, err := s.UpdateSubscriptions([]Subscription{{ID: "s", Name: "new"}, {ID: "gone"}}); err == nil || missing != 1 {
		t.Fatalf("expected write failure, missing=%d err=%v", missing, err)
	}
	if v := s.GetSubscription("s"); v.Name != "" || len(v.Nodes) != 1 {
		t.Fatalf("failed write not rolled back: %+v", v)
	}
}
