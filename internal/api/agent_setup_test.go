package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func publicGet(s *Server, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

func TestAgentSetupScriptFollowsServerTransport(t *testing.T) {
	s := testServer(t)
	w := publicGet(s, "http://192.168.50.142:9090/agent/setup.sh")
	script := w.Body.String()
	if w.Code != 200 || !strings.Contains(script, "SERVER='http://192.168.50.142:9090'") || !strings.Contains(script, "--allow-insecure-http") || strings.Contains(script, "--pinnedpubkey") {
		t.Fatal(w.Code, script)
	}
	if !strings.Contains(script, "</dev/tty") {
		t.Fatal("token prompt must read from the terminal when piped into sh")
	}
	if out, err := exec.Command("sh", "-n", "-c", script).CombinedOutput(); err != nil {
		t.Fatal("script syntax", err, string(out))
	}
	// macOS sh 在 UTF-8 locale 下会把 $VAR 后紧跟的中文字节当作变量名的一部分，变量必须写成 ${VAR}。
	if bare := regexp.MustCompile(`\$[A-Za-z_]`).FindAllString(script, -1); len(bare) > 0 {
		t.Fatal("unbraced shell variables", bare)
	}
	if local := publicGet(s, "http://127.0.0.1:9090/agent/setup.sh").Body.String(); strings.Contains(local, "--allow-insecure-http") {
		t.Fatal("loopback setup must not enable insecure HTTP")
	}

	s.managementFingerprint, s.managementPubkeyPin = strings.Repeat("ab", 32), "sha256//pin="
	tlsScript := publicGet(s, "https://192.168.50.142:9443/agent/setup.sh").Body.String()
	if !strings.Contains(tlsScript, "--pinnedpubkey 'sha256//pin='") || !strings.Contains(tlsScript, "--fingerprint '"+strings.Repeat("ab", 32)+"'") || strings.Contains(tlsScript, "--allow-insecure-http") {
		t.Fatal(tlsScript)
	}
	if w := publicGet(s, "http://attacker.example/agent/setup.sh"); w.Code != 403 {
		t.Fatal("domain Host accepted", w.Code)
	}
}

func TestAgentConnectionSuggestsAllowedNetworks(t *testing.T) {
	f := newAgentFixture(t)
	_, network, _ := net.ParseCIDR("192.168.50.0/24")
	f.s.SetAllowedNetworks([]*net.IPNet{network})
	for host, want := range map[string]string{"192.168.50.142:9090": `["192.168.50.0/24"]`, "127.0.0.1:19090": `[]`} {
		req := httptest.NewRequest("GET", "http://127.0.0.1/api/agent/connection", nil)
		req.Host = host
		req.RemoteAddr = "192.168.50.20:5555"
		req.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.s.router.ServeHTTP(w, req)
		var response struct {
			Data struct {
				LocalOnly bool            `json:"local_only"`
				CIDRs     json.RawMessage `json:"suggested_cidrs"`
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || string(response.Data.CIDRs) != want || response.Data.LocalOnly != strings.HasPrefix(host, "127.") {
			t.Fatal(host, w.Body.String())
		}
	}
}

func TestAgentDownloadServesSelfAndVerifiedReleaseCache(t *testing.T) {
	s := testServer(t)
	self := filepath.Join(t.TempDir(), "sbm")
	if err := os.WriteFile(self, []byte("self-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	s.agentClients.self = self
	native := "sbm-" + runtime.GOOS + "-" + runtime.GOARCH
	if w := publicGet(s, "http://192.168.50.142:9090/agent/download/"+native); agentClientAssets[native] && (w.Code != 200 || w.Body.String() != "self-binary") {
		t.Fatal("native client", w.Code, w.Body.String())
	}
	other := "sbm-darwin-arm64"
	if other == native {
		other = "sbm-linux-amd64"
	}
	binary := []byte("release-binary-" + other)
	sum := sha256.Sum256(binary)
	digest := hex.EncodeToString(sum[:])
	var hits atomic.Int32
	release := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/v0.0.1/SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n%s  install-gateway.sh\n", digest, other, strings.Repeat("0", 64))
		case "/v0.0.1/" + other:
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer release.Close()
	s.agentClients = newAgentClientCache(s.store.GetDataDir(), self, "0.0.1", func() string { return "" })
	s.agentClients.release = release.URL
	for range 2 {
		w := publicGet(s, "http://192.168.50.142:9090/agent/download/"+other)
		if w.Code != 200 || w.Body.String() != string(binary) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if hits.Load() != 2 {
		t.Fatal("cached client must not be downloaded again", hits.Load())
	}
	if w := publicGet(s, "http://192.168.50.142:9090/agent/download/sbm-windows-amd64"); w.Code != 404 {
		t.Fatal("unsupported platform", w.Code)
	}
	// 编码的路径分隔符不能命中下载路由。
	if w := publicGet(s, "http://192.168.50.142:9090/agent/download/..%2fauth.json"); w.Header().Get("Content-Disposition") != "" {
		t.Fatal("path traversal", w.Code)
	}
}

func TestAgentDownloadRejectsMismatchedReleaseAndBacksOff(t *testing.T) {
	s := testServer(t)
	other := "sbm-darwin-arm64"
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		other = "sbm-linux-amd64"
	}
	var hits atomic.Int32
	release := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("1", 64), other)
			return
		}
		_, _ = w.Write([]byte("tampered"))
	}))
	defer release.Close()
	s.agentClients.release = release.URL
	w := publicGet(s, "http://192.168.50.142:9090/agent/download/"+other)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "SHA-256") {
		t.Fatal(w.Code, w.Body.String())
	}
	if files, _ := filepath.Glob(filepath.Join(s.store.GetDataDir(), "agent", "clients", "*", "*")); len(files) != 0 {
		t.Fatal("unverified client cached", files)
	}
	before := hits.Load()
	if w := publicGet(s, "http://192.168.50.142:9090/agent/download/"+other); w.Code != 502 || hits.Load() != before {
		t.Fatal("failed download must back off", w.Code, hits.Load())
	}
}

func TestAgentPublicRoutesRespectAllowedNetworks(t *testing.T) {
	s := testServer(t)
	_, network, _ := net.ParseCIDR("10.0.0.0/8")
	s.SetAllowedNetworks([]*net.IPNet{network})
	req := httptest.NewRequest("GET", "http://192.168.50.142:9090/agent/setup.sh", nil)
	req.RemoteAddr = "192.168.50.20:5555"
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("source CIDR not enforced", w.Code)
	}
}
