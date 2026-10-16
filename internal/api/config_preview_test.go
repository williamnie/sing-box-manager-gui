package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestConfigPreviewBounds(t *testing.T) {
	for _, text := range []string{
		`{"small":true}`, "",
		strings.Repeat("a", 3_300_000),
		strings.Repeat("a", 30_000_000),
		strings.Repeat("短行\n", 10000),
		strings.Repeat("a", configPreviewMaxBytes-1) + "中🙂末尾",
	} {
		preview, info := configPreview(text, []byte(text))
		if len(preview) > configPreviewMaxBytes || strings.Count(preview, "\n") >= configPreviewMaxLines || !utf8.ValidString(preview) {
			t.Fatalf("unbounded or invalid preview: %d bytes", len(preview))
		}
		if !strings.HasPrefix(text, preview) || info.Truncated != (preview != text) || info.PreviewBytes != len(preview) || info.Bytes != len(text) || info.Hash != configHash([]byte(text)) {
			t.Fatalf("preview metadata lost source identity: %+v", info)
		}
	}
}

func TestConfigReviewBoundedResponsesAndCompleteDownloads(t *testing.T) {
	s := testServer(t)
	p := &fakeProcess{path: s.resolvePath(s.store.GetSettings().ConfigPath)}
	s.processManager = p
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	settings.ClashAPISecret = "synthetic-secret-only"
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	values := make([]string, 12000)
	for i := range values {
		values[i] = fmt.Sprintf("rule-%07d.example.test", i)
	}
	if err := s.store.AddRule(storage.Rule{ID: "large", RuleType: "domain_suffix", Values: values, Outbound: "DIRECT", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	candidate, err := s.buildSettings(settings, true)
	if err != nil {
		t.Fatal(err)
	}
	// 只在预览之外改变内容；状态必须按完整原文比较。
	current := strings.ReplaceAll(candidate, values[len(values)-1], "changed-at-end.example.test")
	previous := strings.ReplaceAll(candidate, values[len(values)-1], "previous-at-end.example.test")
	for suffix, content := range map[string]string{"": current, ".previous": previous} {
		if err := os.WriteFile(p.path+suffix, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var diff struct {
		Data struct {
			Current       string            `json:"current"`
			Candidate     string            `json:"candidate"`
			Changed       bool              `json:"changed"`
			CurrentInfo   configPreviewInfo `json:"current_info"`
			CandidateInfo configPreviewInfo `json:"candidate_info"`
		} `json:"data"`
	}
	w := request(s, "GET", "/api/config/diff?view=preview", nil, cookie, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &diff) != nil {
		t.Fatalf("preview failed: %d", w.Code)
	}
	if !diff.Data.Changed || diff.Data.Current != diff.Data.Candidate || !diff.Data.CurrentInfo.Truncated || !diff.Data.CandidateInfo.Truncated || len(diff.Data.Current) > configPreviewMaxBytes || len(diff.Data.Candidate) > configPreviewMaxBytes || diff.Data.CurrentInfo.Hash == diff.Data.CandidateInfo.Hash {
		t.Fatal("preview changed diff semantics or lost its bound")
	}
	var versions struct {
		Data []struct {
			Hash        string            `json:"hash"`
			Bytes       int               `json:"bytes"`
			Preview     string            `json:"preview"`
			PreviewInfo configPreviewInfo `json:"preview_info"`
		} `json:"data"`
	}
	w = request(s, "GET", "/api/config/versions?view=preview", nil, cookie, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &versions) != nil || len(versions.Data) != 2 {
		t.Fatal("version preview failed")
	}
	for _, version := range versions.Data {
		if !version.PreviewInfo.Truncated || version.Hash != version.PreviewInfo.Hash || version.Bytes != version.PreviewInfo.Bytes || len(version.Preview) > configPreviewMaxBytes {
			t.Fatal("version metadata or bounds changed")
		}
	}
	for source, raw := range map[string]string{"current": current, "candidate": candidate, "previous": previous} {
		url := "/api/config/download?source=" + source + "&hash=" + configHash([]byte(raw))
		if request(s, "GET", url, nil, nil, "").Code != 401 {
			t.Fatal("download bypassed authentication")
		}
		w = request(s, "GET", url, nil, cookie, "")
		if w.Code != 200 || w.Body.String() != redactJSON(raw) || !json.Valid(w.Body.Bytes()) || strings.Contains(w.Body.String(), settings.ClashAPISecret) || !strings.Contains(w.Header().Get("Content-Disposition"), source+"-redacted.json") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("incomplete or unsafe %s download: %d", source, w.Code)
		}
		if request(s, "GET", url+"stale", nil, cookie, "").Code != 409 {
			t.Fatal("stale download accepted")
		}
	}
	for _, endpoint := range []string{"/api/config/diff", "/api/config/versions"} {
		w = request(s, "GET", endpoint, nil, cookie, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "changed-at-end.example.test") || strings.Contains(w.Body.String(), settings.ClashAPISecret) {
			t.Fatal("legacy full response was truncated or leaked credentials")
		}
	}
	if err := os.WriteFile(p.path, []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	w = request(s, "GET", "/api/config/diff?view=preview", nil, cookie, "")
	if json.Unmarshal(w.Body.Bytes(), &diff) != nil || diff.Data.Changed || diff.Data.CurrentInfo.Hash != diff.Data.CandidateInfo.Hash {
		t.Fatal("in-sync state was lost")
	}
	if p.applies != 0 || p.checks != 0 || p.running {
		t.Fatal("read-only review changed runtime state")
	}
	stored, err := os.ReadFile(p.path)
	if err != nil || string(stored) != candidate {
		t.Fatal("review truncated persisted configuration")
	}
}

func TestConfigReviewMissingInvalidAndRedaction(t *testing.T) {
	s := testServer(t)
	s.processManager = &fakeProcess{}
	cookie := setup(t, s)
	path := s.resolvePath(s.store.GetSettings().ConfigPath)
	if w := request(s, "GET", "/api/config/diff?view=preview", nil, cookie, ""); w.Code != 200 {
		t.Fatal("missing current must allow candidate preview")
	}
	for _, raw := range []string{
		`{"nested":[{"password":"synthetic-secret","private_key":"synthetic-key"}],"number":9007199254740993}`,
		`{"password":"synthetic-secret"`,
		`{"password":"synthetic-secret"} trailing`,
	} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		w := request(s, "GET", "/api/config/download?source=current&hash="+configHash([]byte(raw)), nil, cookie, "")
		if strings.Contains(w.Body.String(), "synthetic-secret") || strings.Contains(w.Body.String(), "synthetic-key") {
			t.Fatal("download leaked credentials")
		}
		if json.Valid([]byte(raw)) {
			if w.Code != 200 || !strings.Contains(w.Body.String(), "9007199254740993") {
				t.Fatal("download rounded JSON numbers")
			}
		} else if w.Code != 400 {
			t.Fatal("invalid JSON was downloaded")
		}
	}
	if request(s, "GET", "/api/config/download?source=../../other", nil, cookie, "").Code != 400 {
		t.Fatal("unrecognized source accepted")
	}
}

func BenchmarkConfigReviewPayload(b *testing.B) {
	for _, size := range []int{3_300_000, 30_000_000} {
		raw := []byte(`{"route":{"rules":[{"domain_regex":["` + strings.Repeat("a", size) + `"]}]},"password":"synthetic-secret"}`)
		for _, bounded := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/preview=%t", len(raw), bounded), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					text := redactJSON(string(raw))
					if bounded {
						text, _ = configPreview(text, raw)
					}
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.JSON(200, gin.H{"data": gin.H{"current": text, "candidate": text}})
					b.ReportMetric(float64(w.Body.Len()), "response-bytes")
				}
			})
		}
	}
}
