package builder

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestFilterExplicitMembersAndSubscriptionScope(t *testing.T) {
	// 使用 JSON 固定旧客户端及新增字段的输入，先验证最终生成的成员。
	for _, tc := range []struct {
		name, filter string
		want         []string
	}{
		{"legacy", `{"all_nodes":true}`, []string{"家宽", "家宽备用", "手动"}},
		{"exact selection", `{"all_nodes":true,"node_tags":["家宽"]}`, []string{"家宽"}},
		{"empty selection never expands", `{"all_nodes":true,"node_tags":[]}`, nil},
		{"removed node never returns", `{"all_nodes":true,"node_tags":["已删除"]}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var filter storage.Filter
			if err := json.Unmarshal([]byte(tc.filter), &filter); err != nil {
				t.Fatal(err)
			}
			filter.Name, filter.Mode, filter.Enabled = "家庭代理", "selector", true
			b := NewConfigBuilder(storage.DefaultSettings(), []storage.Node{
				{Tag: "家宽", Type: "socks"}, {Tag: "家宽备用", Type: "socks"}, {Tag: "手动", Type: "socks"},
			}, []storage.Filter{filter}, nil, nil)
			var got []string
			for _, o := range b.buildOutbounds() {
				if o["tag"] == filter.Name {
					got = o["outbounds"].([]string)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("members = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterSubscriptionScope(t *testing.T) {
	b := NewConfigBuilder(storage.DefaultSettings(), nil, nil, nil, nil)
	filter := storage.Filter{Subscriptions: []string{"chosen"}}
	for _, tc := range []struct {
		source string
		want   bool
	}{{"chosen", true}, {"other", false}, {"", false}} {
		if got := b.matchFilter(storage.Node{Tag: "node", SubscriptionID: tc.source}, filter); got != tc.want {
			t.Fatalf("source %q: %v", tc.source, got)
		}
	}
	filter.AllNodes = true
	if !b.matchFilter(storage.Node{Tag: "manual"}, filter) {
		t.Fatal("all_nodes must include manual nodes")
	}
}
