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

func TestConnectStoresVerifiedPrivateConnectionAndReusesIt(t *testing.T) {
	dir := t.TempDir()
	token := "sbm_12345678_" + strings.Repeat("a", 64)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"invalid token"}`))
			return
		}
		w.Write([]byte(`{"data":{"api_version":"v1","ok":true}}`))
	}))
	defer server.Close()
	getenv := func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	}
	run := func(input string, args ...string) (int, string) {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr, strings.NewReader(input), getenv)
		result := stdout.String() + stderr.String()
		if strings.Contains(result, token) {
			t.Fatal("token leaked to output")
		}
		return code, result
	}
	sum := sha256.Sum256(server.Certificate().Raw)
	pin := hex.EncodeToString(sum[:])
	if code, out := run(token+"\n", "connect", "--server", server.URL, "--fingerprint", pin); code != ExitOK {
		t.Fatal(code, out)
	}
	path := filepath.Join(dir, "sbm", "connection.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("connection permissions", err)
	}
	before, _ := os.ReadFile(path)
	if code, out := run("", "status"); code != ExitOK {
		t.Fatal("saved connection not reused", code, out)
	}
	if requests != 2 {
		t.Fatal("unexpected request count", requests)
	}
	if code, _ := run("", "--server", "https://elsewhere.example", "status"); code != ExitUsage {
		t.Fatal("saved token allowed for different server")
	}
	if code, _ := run("sbm_wrong\n", "--server", server.URL, "--fingerprint", pin, "connect"); code != ExitAuth {
		t.Fatal("invalid token accepted", code)
	}
	if code, _ := run(token+"\n", "connect", "--server", server.URL, "--fingerprint", strings.Repeat("0", 64)); code != ExitServer {
		t.Fatal("wrong certificate accepted", code)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed reconnect overwrote saved credentials")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("", "status"); code != ExitUsage {
		t.Fatal("insecure profile accepted")
	}
}

func TestConnectionDoesNotSaveIncompatibleServer(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":{}}`)) }))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"connect", "--server", server.URL}, &stdout, &stderr, strings.NewReader("sbm_test\n"), func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	})
	if code != ExitServer {
		t.Fatal(code, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "sbm", "connection.json")); !os.IsNotExist(err) {
		t.Fatal("incompatible server saved")
	}
}

func TestSavedHTTPConnectionWarnsOnlyWhenConnecting(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sbm"), 0700); err != nil {
		t.Fatal(err)
	}
	saved := `{"version":1,"server":"http://192.0.2.10:9","allow_insecure_http":true,"token":"sbm_12345678_` + strings.Repeat("a", 64) + `"}`
	if err := os.WriteFile(filepath.Join(dir, "sbm", "connection.json"), []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--timeout", "200ms", "status"}, &stdout, &stderr, strings.NewReader(""), func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	})
	if code != ExitServer || strings.Contains(stderr.String(), "明文") {
		t.Fatal(code, stderr.String())
	}
}
