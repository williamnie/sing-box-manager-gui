package builder

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestMixedLANDoesNotChangeGatewayRouting(t *testing.T) {
	for _, mode := range []string{"gateway", "dns-bypass"} {
		t.Run(mode, func(t *testing.T) {
			settings := gatewaySettings()
			if mode == "dns-bypass" {
				settings = dnsBypassSettings()
			}
			local, err := gwBuilder(settings, nil).Build()
			if err != nil {
				t.Fatal(err)
			}
			settings.AllowLAN = true
			lan, err := gwBuilder(settings, nil).Build()
			if err != nil {
				t.Fatal(err)
			}
			// 仅 mixed 的监听范围变化；TUN/TProxy、DNS、分流和控制接口必须完全相同。
			local.Inbounds[0].Listen = "0.0.0.0"
			if !reflect.DeepEqual(local, lan) {
				t.Fatal("enabling mixed LAN access changed transparent gateway configuration")
			}
		})
	}
}

func TestMixedLANBindingAcrossDeploymentModes(t *testing.T) {
	for _, mode := range []string{"desktop", "gateway", "dns-bypass"} {
		for _, allowLAN := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/allow_lan=%t", mode, allowLAN), func(t *testing.T) {
				settings := storage.DefaultSettings()
				if mode == "gateway" {
					settings = gatewaySettings()
				} else if mode == "dns-bypass" {
					settings = dnsBypassSettings()
				}
				settings.AllowLAN = allowLAN
				settings.MixedPort = 3712
				settings.ClashAPIPort = 19091
				config, err := gwBuilder(settings, nil).Build()
				if err != nil {
					t.Fatal(err)
				}
				wantListen := "127.0.0.1"
				if allowLAN {
					wantListen = "0.0.0.0"
				}
				var mixed *Inbound
				for i := range config.Inbounds {
					if config.Inbounds[i].Type == "mixed" {
						mixed = &config.Inbounds[i]
					}
				}
				if mixed == nil || mixed.Listen != wantListen || mixed.ListenPort != settings.MixedPort {
					t.Errorf("mixed inbound = %+v, want %s:%d", mixed, wantListen, settings.MixedPort)
				}
				if got := config.Experimental.ClashAPI.ExternalController; got != "127.0.0.1:19091" {
					t.Errorf("controller = %s, want local-only control on configured port", got)
				}
			})
		}
	}
}

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
