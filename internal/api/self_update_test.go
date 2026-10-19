package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.3.7", "v0.3.8", -1, true},
		{"0.3.10", "0.3.9", 1, true},
		{"v1.0", "1.0.0", 0, true},
		{"1.0.0-rc1", "1.0.0", -1, true},
		{"1.0.0", "1.0.0-rc1", 1, true},
		{"dev", "1.0.0", 0, false},
	} {
		got, ok := compareVersions(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("compareVersions(%q, %q) = %d, %v", tc.a, tc.b, got, ok)
		}
	}
}

// updateFixture 提供与 GitHub 同构的发布接口和下载目录，并把 exec 替换为记录调用。
func updateFixture(t *testing.T, tag string, binary []byte) (*Server, *http.Cookie, string, chan string) {
	t.Helper()
	asset := selfUpdateAsset()
	sum := sha256.Sum256(binary)
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "name": tag, "body": "notes", "html_url": "https://example.invalid/" + tag})
	})
	mux.HandleFunc("/download/"+tag+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	mux.HandleFunc("/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	dir := t.TempDir()
	store, err := storage.NewJSONStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "sbm")
	if err := os.WriteFile(exe, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	pm := daemon.NewProcessManager(filepath.Join(dir, "bin/sing-box"), filepath.Join(dir, "generated/config.json"), dir)
	s := NewServer(store, pm, nil, nil, exe, 9090, "0.3.7")
	s.updater.latestAPI = upstream.URL + "/latest"
	s.updater.release = upstream.URL + "/download"
	execs := make(chan string, 1)
	s.execSelf = func(path string, _ []string, _ []string) error {
		execs <- path
		return fmt.Errorf("exec disabled in test")
	}
	return s, setup(t, s), exe, execs
}

func decodeUpdateStatus(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

func TestUpdateCheckNotifiesWithoutDownloading(t *testing.T) {
	s, cookie, exe, _ := updateFixture(t, "v0.3.8", []byte("new"))
	w := request(s, "POST", "/api/update/check", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data := decodeUpdateStatus(t, w.Body.Bytes())
	if data["available"] != true || data["auto_check"] != true {
		t.Fatalf("unexpected status %v", data)
	}
	if latest := data["latest"].(map[string]any); latest["version"] != "0.3.8" {
		t.Fatalf("latest %v", latest)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("check must not replace the binary")
	}
}

func TestUpdateSettingsSurviveSettingsForm(t *testing.T) {
	s, cookie, _, _ := updateFixture(t, "v0.3.8", []byte("new"))
	if w := request(s, "PUT", "/api/update/settings", map[string]bool{"auto_check": false}, cookie, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	form := storage.DefaultSettings()
	if w := request(s, "PUT", "/api/settings", form, cookie, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !s.store.GetSettings().UpdateCheckDisabled {
		t.Fatal("settings form re-enabled update check")
	}
}

func TestUpdateInstallReplacesBinaryAndRestarts(t *testing.T) {
	s, cookie, exe, execs := updateFixture(t, "v0.3.8", []byte("new"))
	if w := request(s, "POST", "/api/update/install", map[string]string{"version": "0.3.8"}, cookie, ""); w.Code != 409 {
		t.Fatalf("install before check: %d", w.Code)
	}
	request(s, "POST", "/api/update/check", nil, cookie, "")
	if w := request(s, "POST", "/api/update/install", map[string]string{"version": "0.3.9"}, cookie, ""); w.Code != 409 {
		t.Fatalf("install of unconfirmed version: %d", w.Code)
	}
	if w := request(s, "POST", "/api/update/install", map[string]string{"version": "0.3.8"}, cookie, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("binary not replaced: %q", b)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm() != 0755 {
		t.Fatalf("mode %v", info.Mode())
	}
	select {
	case path := <-execs:
		if path != exe {
			t.Fatalf("exec %s", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("manager was not restarted")
	}
}

func TestUpdateInstallKeepsBinaryOnDownloadFailure(t *testing.T) {
	s, cookie, exe, _ := updateFixture(t, "v0.3.8", []byte("new"))
	request(s, "POST", "/api/update/check", nil, cookie, "")
	s.updater.release = s.updater.latestAPI // SHA256SUMS 与文件都不可用
	if w := request(s, "POST", "/api/update/install", nil, cookie, ""); w.Code != 502 {
		t.Fatal(w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("failed download replaced the binary")
	}
}

func TestUpdateInstallBlockedInGatewayMode(t *testing.T) {
	s, cookie, _, _ := updateFixture(t, "v0.3.8", []byte("new"))
	settings := s.store.GetSettings()
	settings.DeploymentRole = "gateway"
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	request(s, "POST", "/api/update/check", nil, cookie, "")
	w := request(s, "GET", "/api/update/status", nil, cookie, "")
	if data := decodeUpdateStatus(t, w.Body.Bytes()); data["install_blocked"] == "" || data["available"] != true {
		t.Fatalf("gateway status %v", data)
	}
	if w := request(s, "POST", "/api/update/install", nil, cookie, ""); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
