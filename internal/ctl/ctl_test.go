package ctl

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tokenFile(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("sbm_00000000_"+strings.Repeat("a", 64)+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCtl(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut, strings.NewReader(""), func(string) string { return "" })
	return code, out.String(), errOut.String()
}

func TestRejectsUnsafeTransportAndTokenFile(t *testing.T) {
	good := tokenFile(t, 0600)
	if code, _, _ := runCtl("--server", "http://192.0.2.10:9090", "--token-file", good, "status"); code != ExitUsage {
		t.Fatal("plaintext remote server accepted", code)
	}
	// 显式开启后放行（服务端不可达，因此是网络错误而非用法错误），并输出明文警告。
	if code, _, stderr := runCtl("--server", "http://192.0.2.10:9", "--timeout", "200ms", "--allow-insecure-http", "--token-file", good, "status"); code != ExitServer || !strings.Contains(stderr, "明文") {
		t.Fatal("explicit insecure http not honored", code, stderr)
	}
	if code, _, stderr := runCtl("--token-file", tokenFile(t, 0644), "status"); code != ExitUsage || !strings.Contains(stderr, "chmod 600") {
		t.Fatal("world-readable token accepted", code, stderr)
	}
	if code, _, _ := runCtl("--token-file", filepath.Join(t.TempDir(), "missing"), "status"); code != ExitUsage {
		t.Fatal("missing token accepted", code)
	}
}

func TestExitCodesAndNoRedirect(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/agent/v1/status":
			w.Write([]byte(`{"data":{"ok":true}}`))
		case "/api/agent/v1/config/diff":
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"x","code":"operation_pending"}`))
		case "/api/agent/v1/kernel":
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"x","code":"scope_denied"}`))
		case "/api/agent/v1/versions-redirect", "/api/agent/v1/config/versions":
			http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
		case "/api/agent/v1/changesets":
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("create without idempotency key")
			}
			w.Write([]byte(`{"data":{"id":"1","state":"checked","apply_ready":false,"blocked_reason":"pending"}}`))
		default:
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"bad","code":"bad_request"}`))
		}
	}))
	defer srv.Close()
	tf := tokenFile(t, 0600)
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"status"}, ExitOK},
		{[]string{"diff"}, ExitDecision},
		{[]string{"kernel"}, ExitAuth},
		{[]string{"get", "rule"}, ExitInvalid},
		{[]string{"rule", "delete", "abc"}, ExitDecision},
		{[]string{"versions"}, ExitInvalid},
	}
	for _, c := range cases {
		code, out, _ := runCtl(append([]string{"--server", srv.URL, "--token-file", tf}, c.args...)...)
		if code != c.want {
			t.Fatalf("%v: exit %d want %d (%s)", c.args, code, c.want, out)
		}
	}
	if len(gotAuth) != len(cases) {
		t.Fatalf("redirect followed or retried: %d requests", len(gotAuth))
	}
	if code, _, _ := runCtl("--server", srv.URL, "--token-file", tf, "sub", "add", "--name", "x", "--url", "https://secret"); code != ExitUsage {
		t.Fatal("subscription URL accepted on command line")
	}
}

func TestFingerprintPinning(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	tf := tokenFile(t, 0600)
	if code, out, _ := runCtl("--server", srv.URL, "--token-file", tf, "--fingerprint", hex.EncodeToString(sum[:]), "status"); code != ExitOK {
		t.Fatal("pinned cert rejected", code, out)
	}
	if code, _, _ := runCtl("--server", srv.URL, "--token-file", tf, "--fingerprint", strings.Repeat("0", 64), "status"); code != ExitServer {
		t.Fatal("wrong fingerprint accepted", code)
	}
	if code, _, _ := runCtl("--server", srv.URL, "--token-file", tf, "status"); code != ExitServer {
		t.Fatal("untrusted cert accepted without pin", code)
	}
}
