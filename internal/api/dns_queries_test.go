package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/domaincheck"
)

func TestDNSQueryAPIAuthenticationFiltersAndExport(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/api/dns/queries", "/api/dns/queries/export"} {
		if request(s, "GET", path, nil, nil, "").Code != 401 {
			t.Fatal("DNS history exposed without authentication")
		}
	}
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.DNSQueryLogEnabled = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.store.GetDataDir(), "logs", "singbox.log")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Format("-0700 2006-01-02 15:04:05")
	log := stamp + " INFO [1 0ms] inbound/direct[lan-dns]: inbound packet connection from 192.0.2.10:1234\n" + stamp + " DEBUG [1 0ms] dns: exchange ads.example. IN A\n" + stamp + " DEBUG [1 0ms] dns: exchange =danger.example. IN A\n"
	if err := os.WriteFile(path, []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/dns/queries?source=192.0.2.10&search=ads", nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	future := time.Now().Add(time.Hour).UnixMilli()
	w = request(s, "GET", fmt.Sprintf("/api/dns/queries?since=%d", future), nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("future time filter did not exclude history", w.Body.String())
	}
	w = request(s, "GET", "/api/dns/queries?type=AAAA", nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("query type filter did not exclude A records", w.Body.String())
	}
	for _, query := range []string{"?source=invalid", "?limit=101", "?offset=-1", "?since=wrong", "?since=20&until=10", "?type=bad/type", "?sort=wrong"} {
		if w := request(s, "GET", "/api/dns/queries"+query, nil, cookie, ""); w.Code != 400 {
			t.Fatal("invalid filter accepted", query, w.Body.String())
		}
	}
	w = request(s, "GET", "/api/dns/queries/export", nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ads.example") || !strings.Contains(w.Body.String(), "'=danger.example") {
		t.Fatal("unsafe or incomplete CSV", w.Body.String())
	}
}

func TestDNSDomainClassificationAndRefreshProtection(t *testing.T) {
	s := testServer(t)
	path := filepath.Join(t.TempDir(), "anti-ad.txt")
	if err := os.WriteFile(path, []byte("#TITLE=anti-AD\n#TOTAL_LINES=1\nDOMAIN-SUFFIX,ads.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.dnsDomainCheck = domaincheck.New(path)
	if request(s, "POST", "/api/dns/queries/domain-list/refresh", nil, nil, "").Code != 401 {
		t.Fatal("refresh requires login")
	}
	cookie := setup(t, s)
	if request(s, "POST", "/api/dns/queries/domain-list/refresh", nil, cookie, "https://evil.example").Code != 403 {
		t.Fatal("cross-origin refresh accepted")
	}
	settings := s.store.GetSettings()
	settings.DNSQueryLogEnabled = true
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(s.store.GetDataDir(), "logs", "singbox.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Format("-0700 2006-01-02 15:04:05")
	log := stamp + " INFO [1 0ms] inbound/direct[lan-dns]: inbound packet connection from 192.0.2.10:1234\n" + stamp + " DEBUG [1 0ms] dns: exchange child.ads.example. IN A\n" + stamp + " DEBUG [1 0ms] dns: exchange unknown.example. IN A\n"
	if err := os.WriteFile(logPath, []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/dns/queries?sort=recent", nil, cookie, "")
	var response struct {
		Classification domaincheck.Result `json:"classification"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if !response.Classification.Status.Ready || len(response.Classification.Matches) != 1 || response.Classification.Matches["child.ads.example"].Rule != "ads.example" {
		t.Fatal(response)
	}
	if !reflect.DeepEqual(s.store.GetSettings(), settings) {
		t.Fatal("classification changed settings")
	}
}

func TestDNSQueryTogglePersistsDraftAndKeepsOtherSettings(t *testing.T) {
	s := testServer(t)
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.AutoApply = false
	settings.LogLevel = "warn"
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	w := request(s, "PUT", "/api/dns/queries/settings", map[string]bool{"enabled": true}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !s.store.GetSettings().DNSQueryLogEnabled || s.store.GetSettings().LogLevel != "warn" || s.store.GetSettings().AutoApply {
		t.Fatal("toggle changed unrelated settings")
	}
	if request(s, "PUT", "/api/dns/queries/settings", map[string]string{"enabled": "yes"}, cookie, "").Code != 400 {
		t.Fatal("invalid enable value accepted")
	}
	if request(s, "PUT", "/api/dns/queries/settings", map[string]any{}, cookie, "").Code != 400 {
		t.Fatal("missing enable value accepted")
	}
}
