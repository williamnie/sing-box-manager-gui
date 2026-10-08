// Package domaincheck 提供域名库命中依据，不参与内核拦截或上传查询历史。
package domaincheck

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxDownloadBytes = 8 * 1024 * 1024
const sourceURL = "https://raw.githubusercontent.com/privacy-protection-tools/anti-AD/master/anti-ad-surge.txt"

type Status struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Ready     bool   `json:"ready"`
	Count     int    `json:"count"`
	UpdatedAt int64  `json:"updated_at"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

type Match struct {
	Rule string `json:"rule"`
	Kind string `json:"kind"`
}

type Result struct {
	Status  Status           `json:"status"`
	Matches map[string]Match `json:"matches"`
}

type ruleIndex struct {
	exact, suffix map[string]bool
	version       string
}

type Checker struct {
	mu      sync.RWMutex
	refresh sync.Mutex
	path    string
	url     string
	client  *http.Client
	index   ruleIndex
	status  Status
}

func New(path string) *Checker {
	c := &Checker{path: path, url: sourceURL, client: &http.Client{Timeout: 20 * time.Second}, status: Status{Name: "anti-AD", URL: "https://github.com/privacy-protection-tools/anti-AD"}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return c
	}
	if err == nil {
		defer f.Close()
		var data []byte
		data, err = readLimited(f)
		if err == nil {
			var index ruleIndex
			index, err = parse(data)
			if err == nil {
				var info os.FileInfo
				info, err = f.Stat()
				if err == nil {
					c.install(index, info.ModTime())
				}
			}
		}
	}
	if err != nil {
		c.status.Error = "本地规则库不可用，请重新更新"
	}
	return c
}

func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDownloadBytes+1))
	if err == nil && len(data) > maxDownloadBytes {
		err = errors.New("规则库超过 8 MiB 限制")
	}
	return data, err
}

func validDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
				return false
			}
		}
	}
	return true
}

func parse(data []byte) (ruleIndex, error) {
	index := ruleIndex{exact: make(map[string]bool), suffix: make(map[string]bool)}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	title, declared, count := false, -1, 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "#TITLE=anti-AD":
			title = true
		case strings.HasPrefix(line, "#VER="):
			index.version = strings.TrimPrefix(line, "#VER=")
		case strings.HasPrefix(line, "#TOTAL_LINES="):
			value, err := strconv.Atoi(strings.TrimPrefix(line, "#TOTAL_LINES="))
			if err != nil {
				return index, errors.New("规则库条数无效")
			}
			declared = value
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			kind, domain, ok := strings.Cut(line, ",")
			domain = strings.ToLower(strings.TrimSpace(domain))
			if !ok || !validDomain(domain) {
				return index, errors.New("规则库域名格式无效")
			}
			switch kind {
			case "DOMAIN":
				index.exact[domain] = true
			case "DOMAIN-SUFFIX":
				index.suffix[domain] = true
			default:
				return index, errors.New("规则库包含不支持的匹配方式")
			}
			count++
		}
	}
	if scanner.Err() != nil || !title || count == 0 || declared != count {
		return index, errors.New("规则库为空、不完整或格式不符")
	}
	return index, nil
}

func (c *Checker) install(index ruleIndex, at time.Time) {
	c.index = index
	c.status.Ready, c.status.Error = true, ""
	c.status.Count = len(index.exact) + len(index.suffix)
	c.status.UpdatedAt, c.status.Version = at.UnixMilli(), index.version
}

func (c *Checker) Refresh(ctx context.Context) (err error) {
	c.refresh.Lock()
	defer c.refresh.Unlock()
	defer func() {
		if err != nil {
			c.mu.Lock()
			c.status.Error = "规则库更新失败，请检查网络后重试"
			c.mu.Unlock()
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return errors.New("无法下载 anti-AD 规则库，请检查管理器网络")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("规则库下载返回 HTTP %d", res.StatusCode)
	}
	data, err := readLimited(res.Body)
	if err != nil {
		return err
	}
	index, err := parse(data)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return errors.New("无法创建规则库缓存目录")
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), ".anti-ad-*")
	if err != nil {
		return errors.New("无法写入规则库缓存")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), c.path)
	}
	if err != nil {
		return errors.New("无法保存规则库缓存，保留上次版本")
	}
	c.mu.Lock()
	c.install(index, time.Now())
	c.mu.Unlock()
	return nil
}

func (c *Checker) Check(domains []string) Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := Result{Status: c.status, Matches: make(map[string]Match)}
	for _, original := range domains {
		domain := strings.ToLower(strings.TrimSuffix(original, "."))
		if c.index.exact[domain] {
			result.Matches[original] = Match{Rule: domain, Kind: "domain"}
			continue
		}
		for part := domain; part != ""; {
			if c.index.suffix[part] {
				result.Matches[original] = Match{Rule: part, Kind: "domain_suffix"}
				break
			}
			_, rest, ok := strings.Cut(part, ".")
			if !ok {
				break
			}
			part = rest
		}
	}
	return result
}
