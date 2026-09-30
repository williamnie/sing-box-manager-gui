package api

import (
	"os"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestDNSBypassApplyStopAndSwitch(t *testing.T) {
	s, p, g := prepareGateway(t)
	settings := s.store.GetSettings()
	settings.Gateway.AccessMode = "dns"
	settings.Gateway.StaticRouteConfirmed = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	cookie := setup(t, s)
	for i := 0; i < 2; i++ {
		w := request(s, "POST", "/api/gateway/apply", nil, cookie, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if g.applyCount != 1 || !p.running {
		t.Fatal("apply not idempotent")
	}
	updated := s.store.GetSettings()
	updated.Hosts = []storage.HostEntry{{Domain: "new.lan", IPs: []string{"192.0.2.55"}, Enabled: true}}
	if err := s.store.UpdateSettings(updated); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p.path)
	if strings.Contains(string(raw), `"auto_route"`) || !strings.Contains(string(raw), `"tproxy"`) {
		t.Fatal("incorrect scope")
	}
	settings = s.store.GetSettings()
	settings.Gateway.AccessMode = "full"
	if w := request(s, "PUT", "/api/gateway/settings", settings, cookie, ""); w.Code != 409 {
		t.Fatal("live mode switch accepted", w.Body.String())
	}
	if w := request(s, "POST", "/api/service/stop", nil, cookie, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if g.applied || p.running || s.store.GetSettings().Gateway.Enabled || g.rollbackCount != 1 {
		t.Fatal("stop did not restore own resources")
	}
	if got := s.store.GetSettings().Hosts; len(got) != 1 || got[0].Domain != "new.lan" {
		t.Fatal("ordinary stop lost recent policy edits")
	}
	settings = s.store.GetSettings()
	settings.DeploymentRole = "desktop"
	if w := request(s, "PUT", "/api/gateway/settings", settings, cookie, ""); w.Code != 200 {
		t.Fatal("switch after restore failed", w.Body.String())
	}
}

func TestDNSBypassDraftAndMacHaveNoSystemOperations(t *testing.T) {
	s, p, g := prepareGateway(t)
	s.platform = "darwin"
	settings := s.store.GetSettings()
	settings.Gateway.AccessMode = "dns"
	cookie := setup(t, s)
	if w := request(s, "PUT", "/api/gateway/settings", settings, cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(s, "POST", "/api/gateway/preview", nil, cookie, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "TProxy") && !strings.Contains(w.Body.String(), "tproxy") {
		t.Fatal(w.Body.String())
	}
	if w := request(s, "POST", "/api/gateway/apply", nil, cookie, ""); w.Code != 400 {
		t.Fatal(w.Body.String())
	}
	if p.applies != 0 || g.applyCount != 0 {
		t.Fatal("draft/mac modified resources")
	}
	settings.DeviceGroups = []storage.DeviceGroup{{ID: "s", Policy: "strict"}}
	if w := request(s, "PUT", "/api/gateway/settings", settings, cookie, ""); w.Code != 400 {
		t.Fatal("strict policy silently downgraded")
	}
}

func TestDNSBypassFailedStartRestoresBothSystems(t *testing.T) {
	s, p, g := prepareGateway(t)
	settings := s.store.GetSettings()
	settings.Gateway.AccessMode = "dns"
	settings.Gateway.StaticRouteConfirmed = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	p.failStart = true
	cookie := setup(t, s)
	w := request(s, "POST", "/api/gateway/apply", nil, cookie, "")
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile(p.path)
	if string(raw) != `{"old":true}` || g.applied || p.running || s.store.GetSettings().Gateway.Enabled {
		t.Fatal("failed start leaked applied state")
	}
}
