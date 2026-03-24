package builder

import (
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestConfigBuilder_NodeToOutbound_TUICEnsuresTLS(t *testing.T) {
	b := NewConfigBuilder(storage.DefaultSettings(), nil, nil, nil, nil)

	outbound := b.nodeToOutbound(storage.Node{
		Tag:        "tuic-node",
		Type:       "tuic",
		Server:     "example.com",
		ServerPort: 443,
		Extra: map[string]interface{}{
			"uuid":     "11111111-1111-1111-1111-111111111111",
			"password": "secret",
		},
	})

	tls, ok := outbound["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("outbound[\"tls\"] type = %T, want map[string]interface{}", outbound["tls"])
	}
	if enabled, ok := tls["enabled"].(bool); !ok || !enabled {
		t.Fatalf("tls.enabled = %#v, want true", tls["enabled"])
	}
}
