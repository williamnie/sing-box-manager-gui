package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 显式开启的本地浏览器夹具：临时数据、假内核、回环监听，不连接真实网关。
// SBM_CONFIG_REVIEW_WEB_DIR=/absolute/web/dist go test ./internal/api -run '^TestConfigurationBrowserFixture$' -v -timeout 30m
func TestConfigurationBrowserFixture(t *testing.T) {
	webDir := os.Getenv("SBM_CONFIG_REVIEW_WEB_DIR")
	if webDir == "" {
		t.Skip("仅供本地浏览器性能验收")
	}
	s := testServer(t)
	s.processManager = &fakeProcess{path: s.resolvePath(s.store.GetSettings().ConfigPath)}
	s.gateway = &fakeGateway{}
	cookie := setup(t, s)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__fixture" {
			// 只有预设合成数据；单条超长规则用于覆盖无法靠行数限制的情况。
			count, longLine := 0, false
			switch r.URL.Query().Get("case") {
			case "small":
				count = 8
			case "3mb":
				count = 42000
			case "30mb", "compact":
				count = 420000
			case "long-line":
				longLine = true
			default:
				http.Error(w, "unknown fixture", 400)
				return
			}
			values := make([]string, count)
			for i := range values {
				values[i] = fmt.Sprintf("rule-%07d.example.test", i)
			}
			ruleType := "domain_suffix"
			if longLine {
				values, ruleType = []string{strings.Repeat("a", 15_000_000)}, "domain_keyword"
			}
			_ = s.store.DeleteRule("perf-fixture")
			if err := s.store.AddRule(storage.Rule{ID: "perf-fixture", Name: "Synthetic performance fixture", RuleType: ruleType, Values: values, Outbound: "DIRECT", Enabled: true}); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			candidate, err := s.buildSettings(s.store.GetSettings(), true)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			current := []byte(candidate)
			if r.URL.Query().Get("case") == "compact" {
				var compact bytes.Buffer
				if err := json.Compact(&compact, current); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				current = compact.Bytes()
			}
			configPath := s.resolvePath(s.store.GetSettings().ConfigPath)
			for _, suffix := range []string{"", ".previous"} {
				if err := os.WriteFile(configPath+suffix, current, 0600); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"current_bytes": len(current), "candidate_bytes": len(candidate), "case": r.URL.Query().Get("case")})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			r.AddCookie(cookie)
			s.router.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.FileServer(http.Dir(webDir)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	t.Logf("Configuration browser fixture: http://%s (20 minute lifetime)", listener.Addr())
	<-time.After(20 * time.Minute)
}
