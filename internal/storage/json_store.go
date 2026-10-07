package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JSONStore JSON 文件存储实现
type JSONStore struct {
	dataDir   string
	mu        sync.RWMutex
	data      *AppData
	persisted *AppData
}

// NewJSONStore 创建新的 JSON 存储
func NewJSONStore(dataDir string) (*JSONStore, error) {
	store := &JSONStore{
		dataDir: dataDir,
	}

	// 确保数据目录存在
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 确保 generated 子目录存在
	generatedDir := filepath.Join(dataDir, "generated")
	if err := os.MkdirAll(generatedDir, 0700); err != nil {
		return nil, fmt.Errorf("创建 generated 目录失败: %w", err)
	}

	if err := os.Chmod(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(generatedDir, 0700); err != nil {
		return nil, err
	}
	// 加载数据
	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

// load 加载数据
func (s *JSONStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dataFile := filepath.Join(s.dataDir, "data.json")

	// 如果文件不存在，初始化默认数据
	if _, err := os.Stat(dataFile); os.IsNotExist(err) {
		s.data = &AppData{
			SchemaVersion: 2,
			Subscriptions: []Subscription{},
			ManualNodes:   []ManualNode{},
			Filters:       []Filter{},
			Rules:         []Rule{},
			RuleGroups:    DefaultRuleGroups(),
			Settings:      DefaultSettings(),
		}
		// 新装与重新加载使用相同默认值，避免首次保存被误判为网关拓扑变更。
		NormalizeSettings(s.data.Settings)
		return s.saveInternal()
	}

	// 读取文件
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return fmt.Errorf("读取数据文件失败: %w", err)
	}

	s.data = &AppData{Settings: DefaultSettings()}
	if err := json.Unmarshal(data, s.data); err != nil {
		return fmt.Errorf("解析数据文件失败: %w", err)
	}

	// 确保 Settings 不为空
	if s.data.Settings == nil {
		s.data.Settings = DefaultSettings()
	}

	// 确保 RuleGroups 不为空
	if s.data.RuleGroups == nil && s.data.SchemaVersion < 2 {
		s.data.RuleGroups = DefaultRuleGroups()
	}

	// 迁移旧的路径格式（移除多余的 data/ 前缀）
	needSave := s.data.SchemaVersion < 2 || s.data.Settings.DeploymentRole == ""
	s.data.SchemaVersion = 2
	NormalizeSettings(s.data.Settings)
	if s.data.Settings.SingBoxPath == "data/bin/sing-box" {
		s.data.Settings.SingBoxPath = "bin/sing-box"
		needSave = true
	}
	if s.data.Settings.ConfigPath == "data/generated/config.json" {
		s.data.Settings.ConfigPath = "generated/config.json"
		needSave = true
	}
	if needSave {
		return s.saveInternal()
	}

	s.persisted = clone(s.data)
	if err := os.Chmod(dataFile, 0600); err != nil {
		return err
	}
	return nil
}

// saveInternal 内部保存方法（不加锁）
func (s *JSONStore) saveInternal() (result error) {
	defer func() {
		if result != nil && s.persisted != nil {
			s.data = clone(s.persisted)
		}
	}()
	dataFile := filepath.Join(s.dataDir, "data.json")

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化数据失败: %w", err)
	}

	if err := atomicWrite(dataFile, data); err != nil {
		return fmt.Errorf("写入数据文件失败: %w", err)
	}
	s.data = clone(s.data)
	s.persisted = clone(s.data)
	return nil
}

// Save 保存数据
func (s *JSONStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveInternal()
}

// ==================== 订阅操作 ====================

// GetSubscriptions 获取所有订阅
func (s *JSONStore) GetSubscriptions() []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.Subscriptions)
}

// GetSubscription 获取单个订阅
func (s *JSONStore) GetSubscription(id string) *Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.data.Subscriptions {
		if s.data.Subscriptions[i].ID == id {
			v := clone(s.data.Subscriptions[i])
			return &v
		}
	}
	return nil
}

// AddSubscription 添加订阅
func (s *JSONStore) AddSubscription(sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Subscriptions = append(s.data.Subscriptions, sub)
	return s.saveInternal()
}

// UpdateSubscription 更新订阅
func (s *JSONStore) UpdateSubscription(sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Subscriptions {
		if s.data.Subscriptions[i].ID == sub.ID {
			s.data.Subscriptions[i] = sub
			return s.saveInternal()
		}
	}
	return fmt.Errorf("订阅不存在: %s", sub.ID)
}

// DeleteSubscription 删除订阅
func (s *JSONStore) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Subscriptions {
		if s.data.Subscriptions[i].ID == id {
			s.data.Subscriptions = append(s.data.Subscriptions[:i], s.data.Subscriptions[i+1:]...)
			return s.saveInternal()
		}
	}
	return fmt.Errorf("订阅不存在: %s", id)
}

// ==================== 过滤器操作 ====================

// GetFilters 获取所有过滤器
func (s *JSONStore) GetFilters() []Filter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.Filters)
}

// GetFilter 获取单个过滤器
func (s *JSONStore) GetFilter(id string) *Filter {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.data.Filters {
		if s.data.Filters[i].ID == id {
			v := clone(s.data.Filters[i])
			return &v
		}
	}
	return nil
}

// AddFilter 添加过滤器
func (s *JSONStore) AddFilter(filter Filter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Filters = append(s.data.Filters, filter)
	return s.saveInternal()
}

// UpdateFilter 更新过滤器
func (s *JSONStore) UpdateFilter(filter Filter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Filters {
		if s.data.Filters[i].ID == filter.ID {
			s.data.Filters[i] = filter
			return s.saveInternal()
		}
	}
	return fmt.Errorf("过滤器不存在: %s", filter.ID)
}

// DeleteFilter 删除过滤器
func (s *JSONStore) DeleteFilter(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Filters {
		if s.data.Filters[i].ID == id {
			s.data.Filters = append(s.data.Filters[:i], s.data.Filters[i+1:]...)
			return s.saveInternal()
		}
	}
	return fmt.Errorf("过滤器不存在: %s", id)
}

// ==================== 规则操作 ====================

// GetRules 获取所有自定义规则
func (s *JSONStore) GetRules() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.Rules)
}

// AddRule 添加规则
func (s *JSONStore) AddRule(rule Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Rules = append(s.data.Rules, rule)
	return s.saveInternal()
}

// UpdateRule 更新规则
func (s *JSONStore) UpdateRule(rule Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Rules {
		if s.data.Rules[i].ID == rule.ID {
			s.data.Rules[i] = rule
			return s.saveInternal()
		}
	}
	return fmt.Errorf("规则不存在: %s", rule.ID)
}

// DeleteRule 删除规则
func (s *JSONStore) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Rules {
		if s.data.Rules[i].ID == id {
			s.data.Rules = append(s.data.Rules[:i], s.data.Rules[i+1:]...)
			return s.saveInternal()
		}
	}
	return fmt.Errorf("规则不存在: %s", id)
}

// ==================== 规则组操作 ====================

// GetRuleGroups 获取所有预设规则组
func (s *JSONStore) GetRuleGroups() []RuleGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.RuleGroups)
}

// UpdateRuleGroup 更新规则组
func (s *JSONStore) UpdateRuleGroup(ruleGroup RuleGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.RuleGroups {
		if s.data.RuleGroups[i].ID == ruleGroup.ID {
			s.data.RuleGroups[i] = ruleGroup
			return s.saveInternal()
		}
	}
	return fmt.Errorf("规则组不存在: %s", ruleGroup.ID)
}

// ==================== 设置操作 ====================

// GetSettings 获取设置
func (s *JSONStore) GetSettings() *Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.Settings)
}

// UpdateSettings 更新设置
func (s *JSONStore) UpdateSettings(settings *Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Settings = clone(settings)
	return s.saveInternal()
}

// ==================== 手动节点操作 ====================

// GetManualNodes 获取所有手动节点
func (s *JSONStore) GetManualNodes() []ManualNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.ManualNodes)
}

// AddManualNode 添加手动节点
func (s *JSONStore) AddManualNode(node ManualNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.ManualNodes = append(s.data.ManualNodes, node)
	return s.saveInternal()
}

// UpdateManualNode 更新手动节点
func (s *JSONStore) UpdateManualNode(node ManualNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.ManualNodes {
		if s.data.ManualNodes[i].ID == node.ID {
			s.data.ManualNodes[i] = node
			return s.saveInternal()
		}
	}
	return fmt.Errorf("手动节点不存在: %s", node.ID)
}

// DeleteManualNode 删除手动节点
func (s *JSONStore) DeleteManualNode(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.ManualNodes {
		if s.data.ManualNodes[i].ID == id {
			s.data.ManualNodes = append(s.data.ManualNodes[:i], s.data.ManualNodes[i+1:]...)
			return s.saveInternal()
		}
	}
	return fmt.Errorf("手动节点不存在: %s", id)
}

// ==================== 辅助方法 ====================

// GetAllNodes 获取所有启用的节点（订阅节点 + 手动节点）
func (s *JSONStore) GetAllNodes() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var nodes []Node
	// 添加订阅节点
	for _, sub := range s.data.Subscriptions {
		if sub.Enabled {
			nodes = append(nodes, sub.Nodes...)
		}
	}
	// 添加手动节点
	for _, mn := range s.data.ManualNodes {
		if mn.Enabled {
			nodes = append(nodes, mn.Node)
		}
	}
	return nodes
}

// GetNodesByCountry 按国家获取节点
func (s *JSONStore) GetNodesByCountry(countryCode string) []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var nodes []Node
	// 订阅节点
	for _, sub := range s.data.Subscriptions {
		if sub.Enabled {
			for _, node := range sub.Nodes {
				if node.Country == countryCode {
					nodes = append(nodes, node)
				}
			}
		}
	}
	// 手动节点
	for _, mn := range s.data.ManualNodes {
		if mn.Enabled && mn.Node.Country == countryCode {
			nodes = append(nodes, mn.Node)
		}
	}
	return nodes
}

// GetCountryGroups 获取所有国家节点分组
func (s *JSONStore) GetCountryGroups() []CountryGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()

	countryCount := make(map[string]int)

	// 统计订阅节点
	for _, sub := range s.data.Subscriptions {
		if sub.Enabled {
			for _, node := range sub.Nodes {
				if node.Country != "" {
					countryCount[node.Country]++
				}
			}
		}
	}
	// 统计手动节点
	for _, mn := range s.data.ManualNodes {
		if mn.Enabled && mn.Node.Country != "" {
			countryCount[mn.Node.Country]++
		}
	}

	var groups []CountryGroup
	for code, count := range countryCount {
		groups = append(groups, CountryGroup{
			Code:      code,
			Name:      GetCountryName(code),
			Emoji:     GetCountryEmoji(code),
			NodeCount: count,
		})
	}

	return groups
}

// GetDataDir 获取数据目录
func (s *JSONStore) GetDataDir() string {
	return s.dataDir
}

// Snapshot 返回独立快照，供一致的预览和迁移事务使用。
func (s *JSONStore) Snapshot() *AppData { s.mu.RLock(); defer s.mu.RUnlock(); return clone(s.data) }
func (s *JSONStore) Replace(data *AppData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data
	s.data = clone(data)
	if err := s.saveInternal(); err != nil {
		s.data = previous
		return err
	}
	return nil
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".data-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
