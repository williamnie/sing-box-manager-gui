package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/gateway"
)

type fakeProcess struct {
	path      string
	running   bool
	failStart bool
	applies   int
	checks    int
}

func (p *fakeProcess) ApplyConfig(b []byte) error { p.applies++; return os.WriteFile(p.path, b, 0600) }
func (p *fakeProcess) CheckConfig([]byte) error   { p.checks++; return nil }
func (*fakeProcess) Version() (string, error)     { return "sing-box version 1.14.1", nil }
func (p *fakeProcess) Start() error {
	if p.failStart {
		p.failStart = false
		return errors.New("simulated health failure")
	}
	p.running = true
	return nil
}
func (p *fakeProcess) Stop() error            { p.running = false; return nil }
func (p *fakeProcess) Restart() error         { return p.Start() }
func (p *fakeProcess) Reload() error          { return nil }
func (p *fakeProcess) IsRunning() bool        { return p.running }
func (p *fakeProcess) GetPID() int            { return 0 }
func (p *fakeProcess) SetConfigPath(s string) { p.path = s }

type fakeGateway struct {
	digest                    string
	failAfterApply            bool
	applied                   bool
	applyCount, rollbackCount int
	cancel                    context.CancelFunc
	rollbackCanceled          bool
}

func (g *fakeGateway) Check(context.Context, string, gateway.Config) (gateway.CheckResult, error) {
	return gateway.CheckResult{Ready: true}, nil
}
func (g *fakeGateway) Apply(_ context.Context, _ string, c gateway.Config) (gateway.Status, error) {
	g.applyCount++
	if !c.Enabled {
		return gateway.Status{}, errors.New("not enabled")
	}
	g.applied = true
	plan, _ := gateway.Preview("gateway", c)
	g.digest = plan.ConfigDigest

	if g.cancel != nil {
		g.cancel()
	}
	if g.failAfterApply {
		return gateway.Status{}, errors.New("simulated response lost")
	}
	return gateway.Status{Applied: true}, nil
}
func (g *fakeGateway) Status(context.Context) (gateway.Status, error) {
	return gateway.Status{Applied: g.applied, ConfigDigest: g.digest}, nil
}
func (g *fakeGateway) Rollback(ctx context.Context) (gateway.Status, error) {
	g.rollbackCount++
	g.rollbackCanceled = ctx.Err() != nil
	g.applied = false
	return gateway.Status{}, nil
}
func prepareGateway(t *testing.T) (*Server, *fakeProcess, *fakeGateway) {
	s := testServer(t)
	s.platform = "linux"
	settings := s.store.GetSettings()
	settings.DeploymentRole = "gateway"
	settings.Gateway = gateway.Config{LANInterface: "eth0", UplinkInterface: "eth0", LANAddress: "192.0.2.2", LANCIDRs: []string{"192.0.2.0/24"}, UpstreamGateway: "192.0.2.1", IPv6Mode: "disabled"}
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	p := &fakeProcess{path: filepath.Join(s.store.GetDataDir(), "generated/config.json")}
	if err := os.WriteFile(p.path, []byte(`{"old":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	g := &fakeGateway{}
	s.processManager = p
	s.gateway = g
	return s, p, g
}
func TestGatewayFailedStartRestoresBothSystems(t *testing.T) {
	s, p, g := prepareGateway(t)
	p.failStart = true
	cookie := setup(t, s)
	w := request(s, "POST", "/api/gateway/apply", nil, cookie, "")
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile(p.path)
	if string(raw) != `{"old":true}` || g.applied || p.running || g.rollbackCount != 1 || s.store.GetSettings().Gateway.Enabled {
		t.Fatalf("incomplete compensation: %s helper=%#v core=%#v", raw, g, p)
	}
	if p.checks == 0 {
		t.Fatal("applied without validation")
	}
}
func TestGatewaySavePreviewAndMacNeverMutateSystem(t *testing.T) {
	s, p, g := prepareGateway(t)
	s.platform = "darwin"
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.Gateway.Enabled = true
	w := request(s, "PUT", "/api/gateway/settings", settings, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if s.store.GetSettings().Gateway.Enabled {
		t.Fatal("save enabled gateway")
	}
	w = request(s, "POST", "/api/gateway/preview", map[string]any{"settings": s.store.GetSettings()}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(s, "POST", "/api/gateway/apply", nil, cookie, "")
	if w.Code != 400 {
		t.Fatal(w.Body.String())
	}
	if p.applies != 0 || g.applyCount != 0 {
		t.Fatal("preview/Mac mutated system")
	}
}
func TestGatewayRequiresExplicitApplyAndRepeatedApplyIsIdempotent(t *testing.T) {
	s, p, g := prepareGateway(t)
	cookie := setup(t, s)
	w := request(s, "POST", "/api/config/apply", nil, cookie, "")
	if w.Code != 400 || g.applyCount != 0 {
		t.Fatal("regular apply enables gateway")
	}
	for i := 0; i < 2; i++ {
		w = request(s, "POST", "/api/gateway/apply", nil, cookie, "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if g.applyCount != 1 || !p.running || !s.store.GetSettings().Gateway.Enabled {
		t.Fatal("not idempotent")
	}
	w = request(s, "POST", "/api/gateway/rollback", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	raw, _ := os.ReadFile(p.path)
	if !strings.Contains(string(raw), "old") {
		t.Fatal("rollback lost original")
	}
}

func TestVersionRestoreRequiresReviewedHashAndSameTakeover(t *testing.T) {
	s := testServer(t)
	p := &fakeProcess{path: filepath.Join(s.store.GetDataDir(), "generated/config.json")}
	s.processManager = p
	cookie := setup(t, s)
	candidate, err := s.buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p.path+".previous", []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/api/config/restore", map[string]string{"hash": "stale"}, cookie, "")
	if w.Code != 409 || p.applies != 0 {
		t.Fatal("stale version accepted")
	}
	w = request(s, "POST", "/api/config/restore", map[string]string{"hash": configHash([]byte(candidate))}, cookie, "")
	if w.Code != 200 || p.applies != 1 || s.store.GetSettings().AutoApply {
		t.Fatal(w.Body.String())
	}
}

func TestGatewayLostApplyResponseCompensates(t *testing.T) {
	s, p, g := prepareGateway(t)
	g.failAfterApply = true
	cookie := setup(t, s)
	w := request(s, "POST", "/api/gateway/apply", nil, cookie, "")
	if w.Code != 500 || g.applied || g.rollbackCount != 1 || p.running {
		t.Fatal(w.Code, w.Body.String(), g, p)
	}
	raw, _ := os.ReadFile(p.path)
	if string(raw) != `{"old":true}` {
		t.Fatal("lost response did not restore old config")
	}
}
func TestHelperTopologyDigestMustMatch(t *testing.T) {
	s, _, g := prepareGateway(t)
	cookie := setup(t, s)
	if w := request(s, "POST", "/api/gateway/apply", nil, cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	g.digest = "other-topology"
	w := request(s, "POST", "/api/config/apply", nil, cookie, "")
	if w.Code != 400 {
		t.Fatal("accepted unrelated helper topology")
	}
}
func TestGatewayVersionRestoreRejectsUplinkChange(t *testing.T) {
	s, p, _ := prepareGateway(t)
	cookie := setup(t, s)
	w := request(s, "POST", "/api/gateway/apply", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	raw, _ := os.ReadFile(p.path)
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["route"].(map[string]any)["default_interface"] = "other-uplink"
	previous, _ := json.Marshal(config)
	if err := os.WriteFile(p.path+".previous", previous, 0600); err != nil {
		t.Fatal(err)
	}
	w = request(s, "POST", "/api/config/restore", map[string]string{"hash": configHash(previous)}, cookie, "")
	if w.Code != 409 {
		t.Fatal("uplink version mismatch accepted", w.Body.String())
	}
}

func TestGatewayRecoverySurvivesCanceledRequest(t *testing.T) {
	s, _, g := prepareGateway(t)
	cookie := setup(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.cancel = cancel
	g.failAfterApply = true
	req := httptest.NewRequest("POST", "http://127.0.0.1:9090/api/gateway/apply", nil).WithContext(ctx)
	req.AddCookie(cookie)
	req.RemoteAddr = "127.0.0.1:51234"
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	if w.Code != 500 || g.rollbackCount != 1 || g.rollbackCanceled {
		t.Fatal("recovery used canceled request context", w.Body.String())
	}
}
