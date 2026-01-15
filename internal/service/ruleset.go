package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaobei/singbox-manager/internal/storage"
	"github.com/xiaobei/singbox-manager/pkg/utils"
)

// RuleSetService 规则集服务
type RuleSetService struct {
	store       *storage.JSONStore
	ruleSetDir  string
	downloadMu  sync.Mutex
}

// NewRuleSetService 创建规则集服务
func NewRuleSetService(store *storage.JSONStore, dataDir string) *RuleSetService {
	ruleSetDir := filepath.Join(dataDir, "rulesets")
	os.MkdirAll(ruleSetDir, 0755)
	return &RuleSetService{
		store:      store,
		ruleSetDir: ruleSetDir,
	}
}

// GetRuleSetDir 获取规则集目录
func (s *RuleSetService) GetRuleSetDir() string {
	return s.ruleSetDir
}

// EnsureRuleSets 确保所有需要的规则集都已下载
// 返回需要下载的规则集标签列表和错误
func (s *RuleSetService) EnsureRuleSets(ruleGroups []storage.RuleGroup, rules []storage.Rule) ([]string, error) {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	settings := s.store.GetSettings()
	needed := s.collectNeededRuleSets(ruleGroups, rules)

	var missing []string
	for tag := range needed {
		localPath := filepath.Join(s.ruleSetDir, tag+".srs")
		if !s.isFileValid(localPath) {
			missing = append(missing, tag)
		}
	}

	if len(missing) == 0 {
		return nil, nil
	}

	// 并发下载缺失的规则集
	var wg sync.WaitGroup
	errChan := make(chan error, len(missing))
	semaphore := make(chan struct{}, 5) // 限制并发数

	for _, tag := range missing {
		wg.Add(1)
		go func(tag string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			url := s.buildRuleSetURL(tag, settings)
			localPath := filepath.Join(s.ruleSetDir, tag+".srs")

			if err := s.downloadRuleSet(url, localPath); err != nil {
				errChan <- fmt.Errorf("下载规则集 %s 失败: %w", tag, err)
			}
		}(tag)
	}

	wg.Wait()
	close(errChan)

	// 收集错误
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return missing, fmt.Errorf("部分规则集下载失败: %v", errors)
	}

	return missing, nil
}

// collectNeededRuleSets 收集所有需要的规则集
func (s *RuleSetService) collectNeededRuleSets(ruleGroups []storage.RuleGroup, rules []storage.Rule) map[string]bool {
	needed := make(map[string]bool)

	// 从规则组收集
	for _, rg := range ruleGroups {
		if !rg.Enabled {
			continue
		}
		for _, sr := range rg.SiteRules {
			needed[fmt.Sprintf("geosite-%s", sr)] = true
		}
		for _, ir := range rg.IPRules {
			needed[fmt.Sprintf("geoip-%s", ir)] = true
		}
	}

	// 从自定义规则收集
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.RuleType == "geosite" {
			for _, v := range rule.Values {
				needed[fmt.Sprintf("geosite-%s", v)] = true
			}
		} else if rule.RuleType == "geoip" {
			for _, v := range rule.Values {
				needed[fmt.Sprintf("geoip-%s", v)] = true
			}
		}
	}

	return needed
}

// buildRuleSetURL 构建规则集下载 URL
func (s *RuleSetService) buildRuleSetURL(tag string, settings *storage.Settings) string {
	var url string
	if len(tag) > 7 && tag[:7] == "geosite" {
		// geosite-xxx -> https://github.com/.../geosite-xxx.srs
		url = fmt.Sprintf("%s/%s.srs", settings.RuleSetBaseURL, tag)
	} else if len(tag) > 5 && tag[:5] == "geoip" {
		// geoip-xxx -> https://github.com/.../geoip-xxx.srs
		url = fmt.Sprintf("%s/../rule-set-geoip/%s.srs", settings.RuleSetBaseURL, tag)
	}

	// 如果配置了 GitHub 代理，添加前缀
	if settings.GithubProxy != "" {
		url = settings.GithubProxy + url
	}

	return url
}

// downloadRuleSet 下载规则集文件
func (s *RuleSetService) downloadRuleSet(url, destPath string) error {
	client := utils.GetHTTPClient()

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 先写入临时文件
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	// 原子性替换
	return os.Rename(tmpPath, destPath)
}

// isFileValid 检查文件是否有效（存在且非空且不太旧）
func (s *RuleSetService) isFileValid(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// 文件大小必须大于 0
	if info.Size() == 0 {
		return false
	}

	// 文件不能太旧（超过 7 天认为需要更新）
	if time.Since(info.ModTime()) > 7*24*time.Hour {
		return false
	}

	return true
}

// GetLocalPath 获取规则集的本地路径
func (s *RuleSetService) GetLocalPath(tag string) string {
	return filepath.Join(s.ruleSetDir, tag+".srs")
}

// RefreshAll 刷新所有规则集
func (s *RuleSetService) RefreshAll(ruleGroups []storage.RuleGroup, rules []storage.Rule) error {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	settings := s.store.GetSettings()
	needed := s.collectNeededRuleSets(ruleGroups, rules)

	var wg sync.WaitGroup
	errChan := make(chan error, len(needed))
	semaphore := make(chan struct{}, 5)

	for tag := range needed {
		wg.Add(1)
		go func(tag string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			url := s.buildRuleSetURL(tag, settings)
			localPath := filepath.Join(s.ruleSetDir, tag+".srs")

			if err := s.downloadRuleSet(url, localPath); err != nil {
				errChan <- fmt.Errorf("下载规则集 %s 失败: %w", tag, err)
			}
		}(tag)
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("部分规则集下载失败: %v", errors)
	}

	return nil
}
