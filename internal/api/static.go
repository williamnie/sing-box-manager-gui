package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Vite 产物文件名带内容摘要，可长期缓存；public 目录复制的图标没有摘要，只短期缓存。
var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8}\.[a-z0-9]+$`)

// staticAssets 按需压缩并缓存嵌入资源：每个文件只压缩一次，之后不再占用请求时的 CPU。
type staticAssets struct {
	fsys    fs.FS
	mu      sync.Mutex
	entries map[string]*staticAsset
}

// 原始内容直接从嵌入文件读取，堆上只保留压缩副本。
type staticAsset struct {
	once        sync.Once
	gzip        []byte
	contentType string
	err         error
}

func newStaticAssets(fsys fs.FS) *staticAssets {
	return &staticAssets{fsys: fsys, entries: make(map[string]*staticAsset)}
}

func compressible(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "javascript") || strings.Contains(contentType, "json") || strings.Contains(contentType, "svg")
}

func (a *staticAssets) load(name string) *staticAsset {
	a.mu.Lock()
	entry, ok := a.entries[name]
	if !ok {
		// 只为实际存在的文件建立条目，避免任意路径请求让缓存无限增长。
		if info, err := fs.Stat(a.fsys, name); err != nil || info.IsDir() {
			a.mu.Unlock()
			return nil
		}
		entry = &staticAsset{}
		a.entries[name] = entry
	}
	a.mu.Unlock()
	entry.once.Do(func() {
		raw, err := fs.ReadFile(a.fsys, name)
		if err != nil {
			entry.err = err
			return
		}
		entry.contentType = mime.TypeByExtension(path.Ext(name))
		if entry.contentType == "" {
			entry.contentType = http.DetectContentType(raw)
		}
		if len(raw) < 1024 || !compressible(entry.contentType) {
			return
		}
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(raw); err == nil && w.Close() == nil && buf.Len() < len(raw) {
			entry.gzip = bytes.Clone(buf.Bytes())
		}
	})
	return entry
}

func (a *staticAssets) serve(c *gin.Context) {
	name := strings.TrimPrefix(path.Clean("/"+c.Param("filepath")), "/")
	entry := a.load(name)
	if name == "" || entry == nil || entry.err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	header := c.Writer.Header()
	if hashedAsset.MatchString(name) {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "public, max-age=86400")
	}
	header.Set("Content-Type", entry.contentType)
	if entry.gzip != nil {
		header.Add("Vary", "Accept-Encoding")
		if strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			header.Set("Content-Encoding", "gzip")
			c.Data(http.StatusOK, entry.contentType, entry.gzip)
			return
		}
	}
	file, err := a.fsys.Open(name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()
	content, ok := file.(io.ReadSeeker)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, content)
}
