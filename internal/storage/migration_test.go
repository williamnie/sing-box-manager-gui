package storage

import (
	"github.com/xiaobei/singbox-manager/internal/gateway"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyDataMigratesToDesktopAndPrivateAtomicStorage(t *testing.T) {
	dir := t.TempDir()
	old := `{"settings":{"singbox_path":"data/bin/sing-box","config_path":"data/generated/config.json","mixed_port":2080,"tun_enabled":true,"web_port":9090,"clash_api_port":9091},"rules":[]}`
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewJSONStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := s.GetSettings()
	if v.DeploymentRole != "desktop" || v.Gateway.Enabled || !v.TunEnabled {
		t.Fatalf("unsafe migration %#v", v)
	}
	if s.Snapshot().SchemaVersion != 2 {
		t.Fatal("missing schema")
	}
	st, _ := os.Stat(filepath.Join(dir, "data.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	v.Gateway.Enabled = true
	if s.GetSettings().Gateway.Enabled {
		t.Fatal("getter exposes mutable storage")
	}
}
func TestEmptyRuleGroupsSurviveReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJSONStore(dir)
	d := s.Snapshot()
	d.RuleGroups = []RuleGroup{}
	if err := s.Replace(d); err != nil {
		t.Fatal(err)
	}
	s, err := NewJSONStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.GetRuleGroups()) != 0 {
		t.Fatal("empty imported rules replaced with defaults")
	}
}

func TestLegacyGatewayKeepsFullMeaningAndImportedPolicy(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := s.GetSettings()
	settings.DeploymentRole = "gateway"
	settings.Gateway.AccessMode = ""
	settings.ImportedPolicy = &ImportedPolicy{Final: "legacy-proxy", Rules: []map[string]any{{"domain_suffix": []any{"example.test"}, "outbound": "legacy-proxy"}}}
	if err = s.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	s, err = NewJSONStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s.GetSettings()
	if got.DeploymentRole != "gateway" || got.Gateway.AccessMode != "full" || got.Gateway.Enabled || got.ImportedPolicy == nil || got.ImportedPolicy.Final != "legacy-proxy" {
		t.Fatal("legacy gateway meaning or imported policy changed")
	}
}
func TestDevicesRejectAmbiguousSources(t *testing.T) {
	s := DefaultSettings()
	s.DeviceGroups = []DeviceGroup{{ID: "one", Policy: "direct"}}
	s.Devices = []Device{{ID: "1", Enabled: true, GroupID: "one", Addresses: []string{"192.0.2.0/24"}}, {ID: "2", Enabled: true, GroupID: "one", Addresses: []string{"192.0.2.1"}}}
	if ValidateSettings(s) == nil {
		t.Fatal("overlap accepted")
	}
}

func TestFailedSaveDoesNotChangeMemory(t *testing.T) {
	s, e := NewJSONStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.GetDataDir(), "data.json")
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	previous := s.GetSettings().MixedPort
	settings := s.GetSettings()
	settings.MixedPort = 29999
	if e = s.UpdateSettings(settings); e == nil {
		t.Fatal("expected filesystem error")
	}
	if s.GetSettings().MixedPort != previous {
		t.Fatal("failed save changed memory")
	}
}

func TestDHCPReservationGroupBecomesSourcePolicy(t *testing.T) {
	s := DefaultSettings()
	s.DeviceGroups = []DeviceGroup{{ID: "bt", Policy: "bypass"}}
	s.Gateway.DHCP.Enabled = true
	s.Gateway.DHCP.Reservations = []gateway.DHCPReservation{{MAC: "02:00:00:00:00:01", Address: "192.0.2.10", Group: "bt"}}
	NormalizeSettings(s)
	devices := EffectiveDevices(s)
	if len(devices) != 1 || devices[0].GroupID != "bt" || len(s.Gateway.BypassCIDRs) != 1 || s.Gateway.BypassCIDRs[0] != "192.0.2.10/32" {
		t.Fatal("DHCP grouping did not reach source policy")
	}
	s.Devices = []Device{{ID: "explicit", GroupID: "bt", Addresses: []string{"192.0.2.10"}, Enabled: true}}
	if len(EffectiveDevices(s)) != 1 {
		t.Fatal("equivalent explicit source duplicated")
	}
}
