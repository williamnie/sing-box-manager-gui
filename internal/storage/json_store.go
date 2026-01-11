package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// jsonBufferPool 用于复用 JSON 序列化的 buffer
var jsonBufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 64*1024)) // 预分配 64KB
	},
}

// JSONStore JSON 文件存储实现
type JSONStore struct {
	dataDir string
	mu      sync.RWMutex
	data    *AppData
}

// NewJSONStore 创建新的 JSON 存储
func NewJSONStore(dataDir string) (*JSONStore, error) {
	store := &JSONStore{
		dataDir: dataDir,
	}

	// 确保数据目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 确保 generated 子目录存在
	generatedDir := filepath.Join(dataDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return nil, fmt.Errorf("创建 generated 目录失败: %w", err)
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
			Subscriptions: []Subscription{},
			ManualNodes:   []ManualNode{},
			Filters:       []Filter{},
			Rules:         []Rule{},
			RuleGroups:    DefaultRuleGroups(),
			Settings:      DefaultSettings(),
		}
		return s.saveInternal()
	}

	// 读取文件
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return fmt.Errorf("读取数据文件失败: %w", err)
	}

	s.data = &AppData{}
	if err := json.Unmarshal(data, s.data); err != nil {
		return fmt.Errorf("解析数据文件失败: %w", err)
	}

	// 确保 Settings 不为空
	if s.data.Settings == nil {
		s.data.Settings = DefaultSettings()
	}

	// 确保 RuleGroups 不为空
	if len(s.data.RuleGroups) == 0 {
		s.data.RuleGroups = DefaultRuleGroups()
	}

	// 迁移旧的路径格式（移除多余的 data/ 前缀）
	needSave := false
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

	return nil
}

// saveInternal 内部保存方法（不加锁）
func (s *JSONStore) saveInternal() error {
	dataFile := filepath.Join(s.dataDir, "data.json")

	// 从池中获取 buffer
	buf := jsonBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer jsonBufferPool.Put(buf)

	// 使用 Encoder 写入 buffer（比 MarshalIndent 更高效）
	encoder := json.NewEncoder(buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.data); err != nil {
		return fmt.Errorf("序列化数据失败: %w", err)
	}

	if err := os.WriteFile(dataFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入数据文件失败: %w", err)
	}

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
	return s.data.Subscriptions
}

// GetSubscription 获取单个订阅
func (s *JSONStore) GetSubscription(id string) *Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.data.Subscriptions {
		if s.data.Subscriptions[i].ID == id {
			return &s.data.Subscriptions[i]
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
			// 清零被删除元素，释放内存引用
			last := len(s.data.Subscriptions) - 1
			copy(s.data.Subscriptions[i:], s.data.Subscriptions[i+1:])
			s.data.Subscriptions[last] = Subscription{} // 清零
			s.data.Subscriptions = s.data.Subscriptions[:last]
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
	return s.data.Filters
}

// GetFilter 获取单个过滤器
func (s *JSONStore) GetFilter(id string) *Filter {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.data.Filters {
		if s.data.Filters[i].ID == id {
			return &s.data.Filters[i]
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
			// 清零被删除元素，释放内存引用
			last := len(s.data.Filters) - 1
			copy(s.data.Filters[i:], s.data.Filters[i+1:])
			s.data.Filters[last] = Filter{} // 清零
			s.data.Filters = s.data.Filters[:last]
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
	return s.data.Rules
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
			// 清零被删除元素，释放内存引用
			last := len(s.data.Rules) - 1
			copy(s.data.Rules[i:], s.data.Rules[i+1:])
			s.data.Rules[last] = Rule{} // 清零
			s.data.Rules = s.data.Rules[:last]
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
	return s.data.RuleGroups
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
	return s.data.Settings
}

// UpdateSettings 更新设置
func (s *JSONStore) UpdateSettings(settings *Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Settings = settings
	return s.saveInternal()
}

// ==================== 手动节点操作 ====================

// GetManualNodes 获取所有手动节点
func (s *JSONStore) GetManualNodes() []ManualNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.ManualNodes
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
			// 清零被删除元素，释放内存引用
			last := len(s.data.ManualNodes) - 1
			copy(s.data.ManualNodes[i:], s.data.ManualNodes[i+1:])
			s.data.ManualNodes[last] = ManualNode{} // 清零
			s.data.ManualNodes = s.data.ManualNodes[:last]
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

	// 预估容量以避免多次内存重分配
	capacity := 0
	for _, sub := range s.data.Subscriptions {
		if sub.Enabled {
			capacity += len(sub.Nodes)
		}
	}
	for _, mn := range s.data.ManualNodes {
		if mn.Enabled {
			capacity++
		}
	}

	// 预分配切片容量
	nodes := make([]Node, 0, capacity)

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

// GetAllNodesPtr 获取所有启用节点的指针切片（零拷贝优化）
// 返回的指针直接引用内部数据，调用者不应修改节点内容
func (s *JSONStore) GetAllNodesPtr() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 预估容量
	capacity := 0
	for _, sub := range s.data.Subscriptions {
		if sub.Enabled {
			capacity += len(sub.Nodes)
		}
	}
	for _, mn := range s.data.ManualNodes {
		if mn.Enabled {
			capacity++
		}
	}

	// 预分配指针切片
	nodes := make([]*Node, 0, capacity)

	// 添加订阅节点指针
	for i := range s.data.Subscriptions {
		if s.data.Subscriptions[i].Enabled {
			for j := range s.data.Subscriptions[i].Nodes {
				nodes = append(nodes, &s.data.Subscriptions[i].Nodes[j])
			}
		}
	}

	// 添加手动节点指针
	for i := range s.data.ManualNodes {
		if s.data.ManualNodes[i].Enabled {
			nodes = append(nodes, &s.data.ManualNodes[i].Node)
		}
	}

	return nodes
}

// GetNodesByCountry 按国家获取节点
func (s *JSONStore) GetNodesByCountry(countryCode string) []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 预估容量：先统计符合条件的节点数
	capacity := 0
	for _, sub := range s.data.Subscriptions {
		if sub.Enabled {
			for _, node := range sub.Nodes {
				if node.Country == countryCode {
					capacity++
				}
			}
		}
	}
	for _, mn := range s.data.ManualNodes {
		if mn.Enabled && mn.Node.Country == countryCode {
			capacity++
		}
	}

	// 预分配切片容量
	nodes := make([]Node, 0, capacity)

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
