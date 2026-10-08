package domaincheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = "#TITLE=anti-AD\n#VER=20261008\n#TOTAL_LINES=2\nDOMAIN-SUFFIX,ads.example\nDOMAIN,exact.example\n"

func TestMatchingBoundariesAndCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "anti-ad.txt")
	c := New(path)
	if got := c.Check([]string{"ads.example"}); got.Status.Ready || len(got.Matches) != 0 {
		t.Fatal(got)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Error("must not send queried domains upstream")
		}
		fmt.Fprint(w, fixture)
	}))
	defer server.Close()
	c.url = server.URL
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, checker := range []*Checker{c, New(path)} {
		got := checker.Check([]string{"ADS.EXAMPLE.", "child.ads.example", "badads.example", "ads.example.evil", "exact.example", "child.exact.example", "unknown.example"})
		if !got.Status.Ready || got.Status.Count != 2 || got.Status.UpdatedAt == 0 || len(got.Matches) != 3 {
			t.Fatal(got)
		}
		if got.Matches["child.ads.example"].Rule != "ads.example" || got.Matches["exact.example"].Kind != "domain" {
			t.Fatal(got)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
}

func TestInvalidDownloadsPreserveLastGoodCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anti-ad.txt")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(path)
	for _, body := range []string{"<html>error</html>", "#TITLE=anti-AD\n#TOTAL_LINES=0\n", strings.Replace(fixture, "TOTAL_LINES=2", "TOTAL_LINES=3", 1), strings.Replace(fixture, "DOMAIN,exact.example", "DOMAIN-REGEX,.*", 1), strings.Replace(fixture, "ads.example", "com", 1), strings.Repeat("x", maxDownloadBytes+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c.url = server.URL
		err := c.Refresh(context.Background())
		server.Close()
		if err == nil {
			t.Fatal("invalid rules accepted")
		}
		got := c.Check([]string{"ads.example"})
		if !got.Status.Ready || got.Status.Error == "" || len(got.Matches) != 1 {
			t.Fatal(got)
		}
		data, _ := os.ReadFile(path)
		if string(data) != fixture {
			t.Fatal("last good cache was replaced")
		}
	}
}

func TestHTTPFailureAndCanceledRefresh(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "anti-ad.txt"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	c.url = server.URL
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("HTTP failure accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Refresh(ctx); err == nil {
		t.Fatal("cancellation ignored")
	}
	if c.Check(nil).Status.Ready {
		t.Fatal("failed download reported ready")
	}
}

func TestChecksStayAvailableWhileRefreshing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anti-ad.txt")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(path)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, strings.ReplaceAll(fixture, "ads.example", "new.example"))
	}))
	defer server.Close()
	c.url = server.URL
	go func() { done <- c.Refresh(context.Background()) }()
	<-started
	before := c.Check([]string{"ads.example", "new.example"})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(before.Matches) != 1 || before.Matches["ads.example"].Rule != "ads.example" {
		t.Fatal(before)
	}
	after := c.Check([]string{"ads.example", "new.example"})
	if len(after.Matches) != 1 || after.Matches["new.example"].Rule != "new.example" {
		t.Fatal(after)
	}
}
