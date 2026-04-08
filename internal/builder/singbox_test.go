package builder

import (
	"encoding/json"
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

func TestConfigBuilder_BuildJSON_OmitsLegacyInboundFieldsByDefault(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.TunEnabled = true

	b := NewConfigBuilder(settings, nil, nil, nil, nil)

	configJSON, err := b.BuildJSON()
	if err != nil {
		t.Fatalf("BuildJSON() error = %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	inbounds, ok := config["inbounds"].([]any)
	if !ok {
		t.Fatalf("config[\"inbounds\"] type = %T, want []any", config["inbounds"])
	}
	if len(inbounds) != 2 {
		t.Fatalf("len(inbounds) = %d, want 2", len(inbounds))
	}

	for i, inboundRaw := range inbounds {
		inbound, ok := inboundRaw.(map[string]any)
		if !ok {
			t.Fatalf("inbounds[%d] type = %T, want map[string]any", i, inboundRaw)
		}
		if _, exists := inbound["sniff"]; exists {
			t.Fatalf("inbounds[%d] unexpectedly contains legacy field \"sniff\": %#v", i, inbound["sniff"])
		}
		if _, exists := inbound["sniff_override_destination"]; exists {
			t.Fatalf("inbounds[%d] unexpectedly contains legacy field \"sniff_override_destination\": %#v", i, inbound["sniff_override_destination"])
		}
	}
}

func TestConfigBuilder_WithSingBoxVersion_KeepsLegacyInboundFieldsForPre113(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.TunEnabled = true

	b := NewConfigBuilder(settings, nil, nil, nil, nil).
		WithSingBoxVersion("sing-box version 1.12.12")

	configJSON, err := b.BuildJSON()
	if err != nil {
		t.Fatalf("BuildJSON() error = %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	inbounds, ok := config["inbounds"].([]any)
	if !ok {
		t.Fatalf("config[\"inbounds\"] type = %T, want []any", config["inbounds"])
	}

	for i, inboundRaw := range inbounds {
		inbound, ok := inboundRaw.(map[string]any)
		if !ok {
			t.Fatalf("inbounds[%d] type = %T, want map[string]any", i, inboundRaw)
		}
		if value, exists := inbound["sniff"]; !exists || value != true {
			t.Fatalf("inbounds[%d].sniff = %#v, want true", i, value)
		}
		if value, exists := inbound["sniff_override_destination"]; !exists || value != true {
			t.Fatalf("inbounds[%d].sniff_override_destination = %#v, want true", i, value)
		}
	}
}

func TestCompatProfileFromVersion_UsesModernProfileFor113OrLater(t *testing.T) {
	profile := CompatProfileFromVersion("sing-box version 1.13.5")
	if profile.LegacyInboundFields {
		t.Fatalf("LegacyInboundFields = %v, want false", profile.LegacyInboundFields)
	}
}
