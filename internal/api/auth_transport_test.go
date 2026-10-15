package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLANManagementTransportSecurity(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			s := testServer(t)
			_, network, err := net.ParseCIDR("192.168.50.0/24")
			if err != nil {
				t.Fatal(err)
			}
			s.SetAllowedNetworks([]*net.IPNet{network})
			origin := scheme + "://192.168.50.142:9090"
			newRequest := func(method, path, body string) *http.Request {
				req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
				req.RemoteAddr = "192.168.50.20:51234"
				req.Header.Set("Origin", origin)
				req.Header.Set("Content-Type", "application/json")
				return req
			}
			serve := func(t *testing.T, req *http.Request, want int) *httptest.ResponseRecorder {
				t.Helper()
				w := httptest.NewRecorder()
				s.router.ServeHTTP(w, req)
				if w.Code != want {
					t.Fatalf("%s %s: status %d, want %d: %s", req.Method, req.URL.Path, w.Code, want, w.Body.String())
				}
				return w
			}
			serve(t, newRequest(http.MethodGet, "/api/settings", ""), http.StatusUnauthorized)
			token, err := os.ReadFile(filepath.Join(s.store.GetDataDir(), "setup-token"))
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]string{"password": "test-password-1234", "setup_token": string(token)})
			if err != nil {
				t.Fatal(err)
			}
			serve(t, newRequest(http.MethodPost, "/api/auth/setup", string(body)), http.StatusForbidden)
			// 首次初始化仍要求本机来源，完成后允许 LAN 登录。
			setupRequest := newRequest(http.MethodPost, "/api/auth/setup", string(body))
			setupRequest.RemoteAddr = "127.0.0.1:51234"
			serve(t, setupRequest, http.StatusOK)
			w := serve(t, newRequest(http.MethodPost, "/api/auth/login", `{"password":"test-password-1234"}`), http.StatusOK)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("expected one session cookie, got %d", len(cookies))
			}
			cookie := cookies[0]
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure != (scheme == "https") {
				t.Fatal("session cookie does not match transport security")
			}
			for _, tc := range []struct {
				name   string
				mutate func(*http.Request)
				status int
			}{
				{"authenticated LAN request", func(*http.Request) {}, http.StatusOK},
				{"source outside CIDR", func(req *http.Request) {
					req.RemoteAddr = "192.168.51.20:51234"
					req.Header.Set("X-Forwarded-For", "192.168.50.20")
				}, http.StatusForbidden},
				{"cross origin", func(req *http.Request) {
					req.Header.Set("Origin", scheme+"://192.168.50.99:9090")
				}, http.StatusForbidden},
				{"cross site", func(req *http.Request) {
					req.Header.Set("Sec-Fetch-Site", "cross-site")
				}, http.StatusForbidden},
				{"hostname rebinding", func(req *http.Request) {
					req.Host = "evil.example:9090"
					req.Header.Set("Origin", scheme+"://evil.example:9090")
				}, http.StatusForbidden},
			} {
				t.Run(tc.name, func(t *testing.T) {
					req := newRequest(http.MethodGet, "/api/settings", "")
					req.AddCookie(cookie)
					tc.mutate(req)
					serve(t, req, tc.status)
				})
			}
		})
	}
}
