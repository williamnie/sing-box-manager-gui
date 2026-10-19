package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestStaticAssetsCompressAndCache(t *testing.T) {
	js := strings.Repeat("console.log('sbm');\n", 200)
	assets := newStaticAssets(fstest.MapFS{
		"index-AbCd1234.js": {Data: []byte(js)},
		"favicon.ico":       {Data: []byte{0, 0, 1, 0}},
	})
	r := gin.New()
	r.GET("/assets/*filepath", assets.serve)
	get := func(path, encoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if encoding != "" {
			req.Header.Set("Accept-Encoding", encoding)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := get("/assets/index-AbCd1234.js", "gzip, br")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("unexpected gzip response: %d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(zr); string(body) != js {
		t.Fatal("gzip body mismatch")
	}
	if w = get("/assets/index-AbCd1234.js", ""); w.Header().Get("Content-Encoding") != "" || w.Body.String() != js {
		t.Fatal("identity response mismatch")
	}
	if w = get("/assets/favicon.ico", "gzip"); w.Code != 200 || strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("unhashed asset: %d %v", w.Code, w.Header())
	}
	if w = get("/assets/../missing.js", "gzip"); w.Code != 404 {
		t.Fatalf("missing asset: %d", w.Code)
	}
	if len(assets.entries) != 2 {
		t.Fatal("cache must only track existing files", len(assets.entries))
	}
}
