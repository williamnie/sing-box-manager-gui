package api

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	agentClientRelease = "https://github.com/williamnie/sing-box-manager-gui/releases/download"
	agentClientMaxSize = 200 << 20
	agentClientRetry   = time.Minute
)

// 只提供发布流程实际产出的客户端，名称同时作为缓存文件名。
var agentClientAssets = map[string]bool{"sbm-linux-amd64": true, "sbm-linux-arm64": true, "sbm-darwin-amd64": true, "sbm-darwin-arm64": true}

// agentClientCache 让 Agent 电脑从局域网获取与管理器同版本的 sbm：
// 同平台直接提供管理器自身的二进制；其他平台由管理器下载一次发布文件，按 SHA256SUMS 校验后缓存。
type agentClientCache struct {
	mu       sync.Mutex
	dir      string
	self     string
	version  string
	release  string
	proxy    func() string
	client   *http.Client
	failures map[string]agentClientFailure
}

type agentClientFailure struct {
	at  time.Time
	err error
}

func newAgentClientCache(dataDir, self, version string, proxy func() string) *agentClientCache {
	return &agentClientCache{
		dir:      filepath.Join(dataDir, "agent", "clients", "v"+strings.TrimPrefix(version, "v")),
		self:     self,
		version:  strings.TrimPrefix(version, "v"),
		release:  agentClientRelease,
		proxy:    proxy,
		client:   &http.Client{Timeout: 5 * time.Minute},
		failures: map[string]agentClientFailure{},
	}
}

func (a *agentClientCache) path(name string) (string, error) {
	if name == "sbm-"+runtime.GOOS+"-"+runtime.GOARCH {
		return a.self, nil
	}
	cached := filepath.Join(a.dir, name)
	if info, err := os.Stat(cached); err == nil && info.Mode().IsRegular() {
		return cached, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if info, err := os.Stat(cached); err == nil && info.Mode().IsRegular() {
		return cached, nil
	}
	// 未登录也能访问下载地址，失败后短时间内不重复请求 GitHub。
	if failure, ok := a.failures[name]; ok && time.Since(failure.at) < agentClientRetry {
		return "", failure.err
	}
	if err := a.fetch(name, cached); err != nil {
		err = fmt.Errorf("管理器无法获取 %s（v%s）：%v。可从 Releases 下载该文件放到管理器的 %s 目录后重试", name, a.version, err, a.dir)
		a.failures[name] = agentClientFailure{at: time.Now(), err: err}
		return "", err
	}
	delete(a.failures, name)
	return cached, nil
}

func (a *agentClientCache) get(url string) (*http.Response, error) {
	if proxy := a.proxy(); proxy != "" {
		url = proxy + url
	}
	response, err := a.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("下载失败")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("下载返回 HTTP %d", response.StatusCode)
	}
	return response, nil
}

func (a *agentClientCache) fetch(name, target string) error {
	base := a.release + "/v" + a.version + "/"
	response, err := a.get(base + "SHA256SUMS")
	if err != nil {
		return fmt.Errorf("校验清单%w", err)
	}
	want := ""
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 64<<10))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
		}
	}
	response.Body.Close()
	if len(want) != sha256.Size*2 {
		return fmt.Errorf("校验清单中没有该文件")
	}
	response, err = a.get(base + name)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := os.MkdirAll(a.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(a.dir, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(response.Body, agentClientMaxSize+1))
	if err != nil {
		return fmt.Errorf("下载中断")
	}
	if written > agentClientMaxSize {
		return fmt.Errorf("文件过大")
	}
	if hex.EncodeToString(hash.Sum(nil)) != want {
		return fmt.Errorf("SHA-256 校验失败")
	}
	if err := f.Chmod(0755); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}

// agentDownload 不要求登录：内容与公开发布文件相同，仍受来源网段限制。
func (s *Server) agentDownload(c *gin.Context) {
	name := c.Param("name")
	if !agentClientAssets[name] {
		c.String(http.StatusNotFound, "不支持的平台：%s", strings.TrimPrefix(name, "sbm-"))
		return
	}
	path, err := s.agentClients.path(name)
	if err != nil {
		c.String(http.StatusBadGateway, "%s", err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	c.FileAttachment(path, "sbm")
}
