package api

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentConnectionUsesLoadedServerCertificate(t *testing.T) {
	seed := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer seed.Close()
	pair := seed.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	config, fingerprint, pin, err := managementTLS(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(config.Certificates[0].Certificate[0])
	if fingerprint != hex.EncodeToString(sum[:]) {
		t.Fatal("fingerprint does not match loaded leaf certificate")
	}
	// 加载后文件变化不影响当前监听证书的指纹。
	if err := os.WriteFile(certPath, []byte("changed on disk"), 0600); err != nil {
		t.Fatal(err)
	}
	f := newAgentFixture(t)
	f.s.managementFingerprint, f.s.managementPubkeyPin = fingerprint, pin
	req := httptest.NewRequest("GET", "https://192.168.50.142:19090/api/agent/connection", nil)
	req.AddCookie(f.cookie)
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	f.s.router.ServeHTTP(w, req)
	var response struct {
		Data struct {
			Server, Fingerprint string
			AllowHTTP           bool `json:"allow_insecure_http"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if response.Data.Server != "https://192.168.50.142:19090" || response.Data.Fingerprint != fingerprint || response.Data.AllowHTTP {
		t.Fatal("incorrect connection metadata", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE KEY") || strings.Contains(w.Body.String(), certPath) {
		t.Fatal("certificate material leaked")
	}
}

func TestAgentConnectionAccessAndTransport(t *testing.T) {
	f := newAgentFixture(t)
	if w := f.call("GET", "/api/agent/connection", nil); w.Code != 401 {
		t.Fatal("Agent token accessed admin setup", w.Code)
	}
	for _, tc := range []struct {
		host      string
		status    int
		allowHTTP bool
	}{
		{"192.168.50.142:19090", 200, true},
		{"127.0.0.1:19090", 200, false},
		{"127.0.0.1:bad-port", 400, false},
	} {
		req := httptest.NewRequest("GET", "http://127.0.0.1/api/agent/connection", nil)
		req.Host = tc.host
		req.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.s.router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatal(tc.host, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var response struct {
				Data struct {
					AllowHTTP bool `json:"allow_insecure_http"`
				}
			}
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.AllowHTTP != tc.allowHTTP {
				t.Fatal(tc.host, w.Body.String())
			}
		}
	}
}
