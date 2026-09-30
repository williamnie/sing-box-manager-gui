package api

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, e := storage.NewJSONStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	pm := daemon.NewProcessManager(filepath.Join(dir, "bin/sing-box"), filepath.Join(dir, "generated/config.json"), dir)
	return NewServer(store, pm, nil, nil, "/test/sbm", 9090, "test")
}
func request(s *Server, method, path string, data any, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(data)
	req := httptest.NewRequest(method, "http://127.0.0.1:9090"+path, bytes.NewReader(raw))
	req.RemoteAddr = "127.0.0.1:51234"
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}
func setup(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	token, e := os.ReadFile(filepath.Join(s.store.GetDataDir(), "setup-token"))
	if e != nil {
		t.Fatal(e)
	}
	w := request(s, "POST", "/api/auth/setup", map[string]string{"password": "test-password-1234", "setup_token": string(token)}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func TestAPIsRequireLoginAndSetupIsSingleUse(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/api/settings", "/api/subscriptions", "/api/config/preview", "/api/monitor/logs", "/api/gateway/status"} {
		w := request(s, "GET", path, nil, nil, "")
		if w.Code != 401 {
			t.Fatalf("%s status %d", path, w.Code)
		}
	}
	cookie := setup(t, s)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	w := request(s, "GET", "/api/settings", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, e := os.Stat(filepath.Join(s.store.GetDataDir(), "setup-token")); !os.IsNotExist(e) {
		t.Fatal("setup token not consumed")
	}
	w = request(s, "POST", "/api/auth/logout", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 401 {
		t.Fatal("logout did not revoke session")
	}
}
func TestCrossOriginAndHostRebindingDenied(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	w := request(s, "PUT", "/api/settings", storage.DefaultSettings(), cookie, "https://evil.example")
	if w.Code != 403 {
		t.Fatal("CSRF allowed")
	}
	req := httptest.NewRequest("GET", "http://evil.example/api/settings", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("DNS rebinding allowed")
	}
}
func TestSettingsCannotEnableGatewayOrChangeExecutable(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	v := s.store.GetSettings()
	v.SingBoxPath = "/bin/sh"
	if request(s, "PUT", "/api/settings", v, cookie, "").Code != 400 {
		t.Fatal("arbitrary binary allowed")
	}
	v = s.store.GetSettings()
	v.Gateway.Enabled = true
	v.DeploymentRole = "gateway"
	if request(s, "PUT", "/api/settings", v, cookie, "").Code != 400 {
		t.Fatal("implicit gateway enabled")
	}
}
func TestCorruptAuthFailsClosed(t *testing.T) {
	s := testServer(t)
	if e := os.WriteFile(filepath.Join(s.store.GetDataDir(), "auth.json"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := newAuth(s.store.GetDataDir()); e == nil {
		t.Fatal("invalid auth accepted")
	}
	s.auth = nil
	if request(s, "GET", "/api/settings", nil, nil, "").Code != 503 {
		t.Fatal("auth unavailable opened access")
	}
}
func TestMigrationPreviewRedactsAndRequiresFreshHash(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	config := `{"outbounds":[{"type":"socks","tag":"Proxy","server":"proxy.example","server_port":1080,"password":"sensitive-password"}],"route":{"final":"Proxy"}}`
	w := request(s, "POST", "/api/migration/preview", map[string]string{"config": config}, cookie, "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("sensitive-password")) {
		t.Fatal(w.Body.String())
	}
	w = request(s, "POST", "/api/migration/import", map[string]any{"config": config, "hash": "stale", "acknowledge_omitted": true}, cookie, "")
	if w.Code != 400 {
		t.Fatal("stale preview imported")
	}
}

func TestRecoveryDoesNotReturnPanicDetails(t *testing.T) {
	s := testServer(t)
	s.router.GET("/panic-fixture", func(c *gin.Context) { panic("private-panic-value") })
	w := request(s, "GET", "/panic-fixture", nil, nil, "")
	if w.Code != 500 || bytes.Contains(w.Body.Bytes(), []byte("private-panic-value")) {
		t.Fatal(w.Code, w.Body.String())
	}
}
