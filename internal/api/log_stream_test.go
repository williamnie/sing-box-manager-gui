package api

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

func logTestServer(t *testing.T) (*Server, *httptest.Server, *http.Cookie, string) {
	t.Helper()
	s := testServer(t)
	cookie := setup(t, s)
	router := gin.New()
	router.GET("/stream", s.requireAuth, s.streamLogs)
	router.GET("/export", s.requireAuth, s.exportLogs)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	path := filepath.Join(s.store.GetDataDir(), "logs", "singbox.log")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	return s, ts, cookie, path
}
func logGet(t *testing.T, ts *httptest.Server, path string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func TestLogStreamRequiresAuthAndValidInputs(t *testing.T) {
	_, ts, cookie, _ := logTestServer(t)
	for _, item := range []struct {
		path   string
		cookie *http.Cookie
		code   int
	}{{"/stream?source=singbox", nil, 401}, {"/stream?source=../sbm", cookie, 400}, {"/stream?source=singbox&cursor=bad", cookie, 400}, {"/export?source=singbox&lines=999999", cookie, 400}} {
		res := logGet(t, ts, item.path, item.cookie)
		res.Body.Close()
		if res.StatusCode != item.code {
			t.Fatalf("%s = %d want %d", item.path, res.StatusCode, item.code)
		}
	}
}
func TestLogExportRedactsAndBoundsTail(t *testing.T) {
	_, ts, cookie, path := logTestServer(t)
	if err := os.WriteFile(path, []byte("old\nINFO token=export-secret\nINFO https://example.com/private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res := logGet(t, ts, "/export?source=singbox&lines=2", cookie)
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(data), "old\n") || strings.Contains(string(data), "export-secret") || strings.Contains(string(data), "example.com") {
		t.Fatalf("invalid export: %d %s", res.StatusCode, data)
	}
	if !strings.Contains(res.Header.Get("Content-Disposition"), "attachment") {
		t.Fatal("missing download header")
	}
}
func TestLogStreamCursorReconnectAndSessionRevocation(t *testing.T) {
	s, ts, cookie, path := logTestServer(t)
	if err := os.WriteFile(path, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res := logGet(t, ts, "/stream?source=singbox", cookie)
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	readEvent := func() (string, logger.LogBatch) {
		t.Helper()
		event := ""
		batch := logger.LogBatch{}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") {
				_ = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &batch)
			}
			if line == "\n" && event != "" {
				return event, batch
			}
		}
	}
	name, batch := readEvent()
	if name != "logs" || len(batch.Lines) != 1 {
		t.Fatalf("first event: %s %#v", name, batch)
	}
	res.Body.Close()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("second\n")
	f.Close()
	res2 := logGet(t, ts, "/stream?source=singbox&cursor="+batch.Cursor, cookie)
	defer res2.Body.Close()
	reader = bufio.NewReader(res2.Body)
	name, batch = readEvent()
	if name != "logs" || strings.Join(batch.Lines, "") != "second" {
		t.Fatalf("reconnect: %s %#v", name, batch)
	}
	s.auth.mu.Lock()
	delete(s.auth.sessions, sha256.Sum256([]byte(cookie.Value)))
	s.auth.mu.Unlock()
	done := make(chan string, 1)
	go func() { data, _ := io.ReadAll(reader); done <- string(data) }()
	select {
	case data := <-done:
		if !strings.Contains(data, "auth_expired") {
			t.Fatalf("session did not terminate: %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revoked session continues streaming")
	}
}
