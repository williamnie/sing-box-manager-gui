package builder

import (
	"github.com/xiaobei/singbox-manager/internal/storage"
	"strings"
	"testing"
)

func TestBuiltinPanelKeepsControlLocalAndRemovesExternalUI(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.AllowLAN = true
	settings.ClashUIPath = "old-zashboard"
	config, err := NewConfigBuilder(settings, nil, nil, nil, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	clash := config.Experimental.ClashAPI
	if !strings.HasPrefix(clash.ExternalController, "127.0.0.1:") || clash.ExternalUI != "" || clash.ExternalUIDownloadURL != "" {
		t.Fatalf("external panel dependency remains: %+v", clash)
	}
	if !config.Experimental.CacheFile.Enabled {
		t.Fatal("selection cache removed")
	}
	if len(config.Inbounds) == 0 || config.Inbounds[0].Listen != "0.0.0.0" {
		t.Fatal("panel migration changed mixed LAN binding")
	}
}

func TestPanelLogLevelIsValidatedAndApplied(t *testing.T) {
	settings := storage.DefaultSettings()
	settings.LogLevel = "debug"
	config, err := NewConfigBuilder(settings, nil, nil, nil, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	if config.Log.Level != "debug" {
		t.Fatal("debug not applied")
	}
	settings.LogLevel = "invalid"
	if err := storage.ValidateSettings(settings); err == nil {
		t.Fatal("invalid log level accepted")
	}
}
