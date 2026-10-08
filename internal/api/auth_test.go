package api

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestChangePasswordRevokesSessionsAndSurvivesRestart(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	other := request(s, "POST", "/api/auth/login", map[string]string{"password": "test-password-1234"}, nil, "")
	if other.Code != 200 {
		t.Fatal(other.Code, other.Body.String())
	}
	newPassword := "my-new-password-123"
	w := request(s, "POST", "/api/auth/password", map[string]string{"current_password": "test-password-1234", "new_password": newPassword}, cookie, "")
	if w.Code != 200 {
		t.Fatalf("change password: %d %s", w.Code, w.Body.String())
	}
	for _, oldCookie := range []*http.Cookie{cookie, other.Result().Cookies()[0]} {
		if request(s, "GET", "/api/settings", nil, oldCookie, "").Code != 401 {
			t.Fatal("password change did not revoke existing session")
		}
	}
	data, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "auth.json"))
	if err != nil || bytes.Contains(data, []byte(newPassword)) {
		t.Fatal("password must only be stored as a hash", err)
	}
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if request(s, "POST", "/api/auth/login", map[string]string{"password": "test-password-1234"}, nil, "").Code != 401 {
		t.Fatal("old password still accepted")
	}
	if w = request(s, "POST", "/api/auth/login", map[string]string{"password": newPassword}, nil, ""); w.Code != 200 {
		t.Fatal("new password failed after restart", w.Code)
	}
}

func TestChangePasswordRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name, current, next, origin string
		unauthenticated             bool
		code                        int
	}{
		{name: "no session", current: "test-password-1234", next: "new-password-123", unauthenticated: true, code: 401},
		{name: "wrong current password", current: "wrong-password", next: "new-password-123", code: 403},
		{name: "short password", current: "test-password-1234", next: "short", code: 400},
		{name: "over bcrypt byte limit", current: "test-password-1234", next: strings.Repeat("中", 25), code: 400},
		{name: "cross origin", current: "test-password-1234", next: "new-password-123", origin: "https://evil.example", code: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t)
			cookie := setup(t, s)
			requestCookie := cookie
			if tc.unauthenticated {
				requestCookie = nil
			}
			w := request(s, "POST", "/api/auth/password", map[string]string{"current_password": tc.current, "new_password": tc.next}, requestCookie, tc.origin)
			if w.Code != tc.code {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.code, w.Body.String())
			}
			if request(s, "GET", "/api/settings", nil, cookie, "").Code != 200 {
				t.Fatal("rejected request revoked the session")
			}
		})
	}
}

func TestChangePasswordWriteFailurePreservesCredentials(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	// 使用目录占据目标文件，稳定模拟原子替换失败（也适用于 root 运行测试）。
	path := filepath.Join(s.store.GetDataDir(), "auth.json")
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/api/auth/password", map[string]string{"current_password": "test-password-1234", "new_password": "new-password-123"}, cookie, "")
	if w.Code != 500 {
		t.Fatalf("got %d, want write failure", w.Code)
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 200 {
		t.Fatal("write failure revoked session")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".backup", path); err != nil {
		t.Fatal(err)
	}
	if request(s, "POST", "/api/auth/login", map[string]string{"password": "test-password-1234"}, nil, "").Code != 200 {
		t.Fatal("write failure changed password")
	}
}

func TestChangePasswordRateLimited(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	for i := 0; i < 5; i++ {
		w := request(s, "POST", "/api/auth/password", map[string]string{"current_password": "incorrect", "new_password": "new-password-123"}, cookie, "")
		want := 403
		if i == 4 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: got %d, want %d", i+1, w.Code, want)
		}
	}
}

func TestConcurrentLoginCannotOutlivePasswordChange(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	var login, change *httptest.ResponseRecorder
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		login = request(s, "POST", "/api/auth/login", map[string]string{"password": "test-password-1234"}, nil, "")
	}()
	go func() {
		defer wg.Done()
		<-start
		change = request(s, "POST", "/api/auth/password", map[string]string{"current_password": "test-password-1234", "new_password": "new-password-123"}, cookie, "")
	}()
	close(start)
	wg.Wait()
	if change.Code != 200 {
		t.Fatal("password change failed", change.Code)
	}
	if login.Code == 200 {
		if request(s, "GET", "/api/settings", nil, login.Result().Cookies()[0], "").Code != 401 {
			t.Fatal("concurrent login left a valid session after password change")
		}
	} else if login.Code != 401 {
		t.Fatal("unexpected login response", login.Code)
	}
}

func TestRememberedSessionSurvivesRestartAndLogout(t *testing.T) {
	s := testServer(t)
	ordinary := setup(t, s)
	w := request(s, "POST", "/api/auth/login", map[string]any{"password": "test-password-1234", "remember_me": true}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != 30*24*3600 || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("invalid remembered cookie attributes")
	}
	data, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "auth.json"))
	if err != nil || bytes.Contains(data, []byte(cookie.Value)) {
		t.Fatal("session token must not be stored in plaintext", err)
	}
	info, err := os.Stat(filepath.Join(s.store.GetDataDir(), "auth.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe auth file permissions", err)
	}
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 200 {
		t.Fatal("remembered session lost after restart")
	}
	if request(s, "GET", "/api/settings", nil, ordinary, "").Code != 401 {
		t.Fatal("ordinary session persisted across restart")
	}
	if request(s, "POST", "/api/auth/logout", nil, cookie, "").Code != 200 {
		t.Fatal("logout failed")
	}
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 401 {
		t.Fatal("logged out session resurrected after restart")
	}
}

func TestRememberedSessionRevokedByPasswordChange(t *testing.T) {
	s := testServer(t)
	setup(t, s)
	w := request(s, "POST", "/api/auth/login", map[string]any{"password": "test-password-1234", "remember_me": true}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	w = request(s, "POST", "/api/auth/password", map[string]string{"current_password": "test-password-1234", "new_password": "new-password-123"}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var err error
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 401 {
		t.Fatal("password change resurrected remembered session")
	}
}

func TestExpiredRememberedSessionRejectedAfterRestart(t *testing.T) {
	s := testServer(t)
	setup(t, s)
	w := request(s, "POST", "/api/auth/login", map[string]any{"password": "test-password-1234", "remember_me": true}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	path := filepath.Join(s.store.GetDataDir(), "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Hash     string `json:"password_hash"`
		Sessions []struct {
			TokenHash string    `json:"token_hash"`
			Expires   time.Time `json:"expires"`
		} `json:"remembered_sessions"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Sessions) != 1 {
		t.Fatal("expected one remembered session")
	}
	record.Sessions[0].Expires = time.Now().Add(-time.Second)
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 401 {
		t.Fatal("expired remembered session accepted")
	}
}

func TestRememberedSetupAndSessionRotation(t *testing.T) {
	s := testServer(t)
	token, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/api/auth/setup", map[string]any{"password": "test-password-1234", "setup_token": string(token), "remember_me": true}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	remembered := w.Result().Cookies()[0]
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if request(s, "GET", "/api/settings", nil, remembered, "").Code != 200 {
		t.Fatal("setup did not remember session")
	}
	w = request(s, "POST", "/api/auth/login", map[string]any{"password": "test-password-1234", "remember_me": false}, remembered, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	ordinary := w.Result().Cookies()[0]
	if ordinary.MaxAge != 8*3600 || ordinary.Value == remembered.Value {
		t.Fatal("session was not rotated to ordinary login")
	}
	s.auth, err = newAuth(s.store.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range []*http.Cookie{remembered, ordinary} {
		if request(s, "GET", "/api/settings", nil, cookie, "").Code != 401 {
			t.Fatal("rotated session survived restart")
		}
	}
}

func TestRememberedSessionWriteFailuresDoNotClaimSuccess(t *testing.T) {
	s := testServer(t)
	setup(t, s)
	w := request(s, "POST", "/api/auth/login", map[string]any{"password": "test-password-1234", "remember_me": true}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	path := filepath.Join(s.store.GetDataDir(), "auth.json")
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	w = request(s, "POST", "/api/auth/logout", nil, cookie, "")
	if w.Code != 500 || len(w.Result().Cookies()) != 0 {
		t.Fatal("failed revocation claimed successful logout")
	}
	w = request(s, "POST", "/api/auth/login", map[string]any{"password": "test-password-1234", "remember_me": true}, cookie, "")
	if w.Code != 500 || len(w.Result().Cookies()) != 0 {
		t.Fatal("failed persistence issued a session cookie")
	}
	if request(s, "GET", "/api/settings", nil, cookie, "").Code != 200 {
		t.Fatal("failed persistence changed existing session")
	}
	w = request(s, "POST", "/api/auth/login", map[string]string{"password": "test-password-1234"}, nil, "")
	if w.Code != 200 {
		t.Fatal("ordinary login unexpectedly requires a writable auth directory", w.Code)
	}
}

func TestCorruptRememberedSessionFailsClosed(t *testing.T) {
	s := testServer(t)
	setup(t, s)
	data, err := json.Marshal(map[string]any{"password_hash": string(s.auth.hash), "remembered_sessions": []map[string]any{{"token_hash": "not-a-hash", "expires": time.Now().Add(time.Hour)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.store.GetDataDir(), "auth.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newAuth(s.store.GetDataDir()); err == nil {
		t.Fatal("corrupt session record accepted")
	}
}
