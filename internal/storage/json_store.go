package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	dataFileName      = "data.json"
	dataBackupName    = "data.json.bak"
	asyncSaveInterval = 1 * time.Second
)

// jsonBufferPool 用于复用 JSON 序列化的 buffer
var jsonBufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 64*1024)) // 预分配 64KB
	},
}

// JSONStore JSON 文件存储实现
// 使用 Copy-on-Write + 版本号 + 异步刷盘队列
type JSONStore struct {
	dataDir string

	mu        sync.Mutex
	persistMu sync.Mutex

	data atomic.Value // *AppData

	version          atomic.Uint64 // 内存数据版本号
	persistedVersion atomic.Uint64 // 已落盘版本号

	saveCh     chan struct{}
	stopCh     chan struct{}
	workerDone chan struct{}
	closed     atomic.Bool
}

// NewJSONStore 创建新的 JSON 存储
func NewJSONStore(dataDir string) (*JSONStore, error) {
	store := &JSONStore{
		dataDir:    dataDir,
		saveCh:     make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
		workerDone: make(chan struct{}),
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
	data, err := store.loadData()
	if err != nil {
		return nil, err
	}

	store.data.Store(data)
	store.version.Store(1)
	store.persistedVersion.Store(1)

	go store.saveWorker()

	return store, nil
}

// Close 关闭存储并强制刷盘
func (s *JSONStore) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(s.stopCh)
	<-s.workerDone

	s.mu.Lock()
	snapshot := s.currentData()
	targetVersion := s.version.Load()
	s.mu.Unlock()

	if err := s.persistToDisk(snapshot); err != nil {
		return err
	}
	s.markPersisted(targetVersion)
	return nil
}

// GetVersion 获取当前数据版本号
func (s *JSONStore) GetVersion() uint64 {
	return s.version.Load()
}

// loadData 加载数据
func (s *JSONStore) loadData() (*AppData, error) {
	dataFile := filepath.Join(s.dataDir, dataFileName)

	// 如果文件不存在，初始化默认数据
	if _, err := os.Stat(dataFile); os.IsNotExist(err) {
		defaultData := defaultAppData()
		if err := s.persistToDisk(defaultData); err != nil {
			return nil, err
		}
		return defaultData, nil
	}

	data, err := os.ReadFile(dataFile)
	if err != nil {
		return nil, fmt.Errorf("读取数据文件失败: %w", err)
	}

	loaded := &AppData{}
	if err := json.Unmarshal(data, loaded); err != nil {
		backupData, backupErr := s.loadFromBackup()
		if backupErr != nil {
			return nil, fmt.Errorf("解析数据文件失败: %w", err)
		}

		if err := s.persistToDisk(backupData); err != nil {
			return nil, err
		}
		return backupData, nil
	}

	needSave := false

	// 确保 Settings 不为空
	if loaded.Settings == nil {
		loaded.Settings = DefaultSettings()
		needSave = true
	}

	// 确保 RuleGroups 不为空
	if len(loaded.RuleGroups) == 0 {
		loaded.RuleGroups = DefaultRuleGroups()
		needSave = true
	}

	// 迁移旧的路径格式（移除多余的 data/ 前缀）
	if loaded.Settings.SingBoxPath == "data/bin/sing-box" {
		loaded.Settings.SingBoxPath = "bin/sing-box"
		needSave = true
	}
	if loaded.Settings.ConfigPath == "data/generated/config.json" {
		loaded.Settings.ConfigPath = "generated/config.json"
		needSave = true
	}

	if needSave {
		if err := s.persistToDisk(loaded); err != nil {
			return nil, err
		}
	}

	return loaded, nil
}

// loadFromBackup 从备份恢复数据
func (s *JSONStore) loadFromBackup() (*AppData, error) {
	backupFile := filepath.Join(s.dataDir, dataBackupName)

	data, err := os.ReadFile(backupFile)
	if err != nil {
		return nil, fmt.Errorf("读取备份文件失败: %w", err)
	}

	loaded := &AppData{}
	if err := json.Unmarshal(data, loaded); err != nil {
		return nil, fmt.Errorf("解析备份文件失败: %w", err)
	}

	if loaded.Settings == nil {
		loaded.Settings = DefaultSettings()
	}
	if len(loaded.RuleGroups) == 0 {
		loaded.RuleGroups = DefaultRuleGroups()
	}

	return loaded, nil
}

// persistToDisk 持久化到磁盘（原子写入 + 备份）
func (s *JSONStore) persistToDisk(data *AppData) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	dataFile := filepath.Join(s.dataDir, dataFileName)
	tmpFile := dataFile + ".tmp"

	buf := jsonBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer jsonBufferPool.Put(buf)

	encoder := json.NewEncoder(buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("序列化数据失败: %w", err)
	}

	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}

	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("同步临时文件失败: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	if err := s.createBackup(dataFile); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	if err := os.Rename(tmpFile, dataFile); err != nil {
		// Windows 场景下 rename 到已存在文件可能失败
		_ = os.Remove(dataFile)
		if retryErr := os.Rename(tmpFile, dataFile); retryErr != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("原子替换数据文件失败: %w", retryErr)
		}
	}

	return nil
}

// createBackup 创建 data.json 备份文件
func (s *JSONStore) createBackup(dataFile string) error {
	data, err := os.ReadFile(dataFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取旧数据文件失败: %w", err)
	}

	backupFile := filepath.Join(s.dataDir, dataBackupName)
	if err := os.WriteFile(backupFile, data, 0644); err != nil {
		return fmt.Errorf("写入备份文件失败: %w", err)
	}

	return nil
}

// saveWorker 后台刷盘协程
func (s *JSONStore) saveWorker() {
	ticker := time.NewTicker(asyncSaveInterval)
	defer ticker.Stop()
	defer close(s.workerDone)

	for {
		select {
		case <-s.saveCh:
			if err := s.flushPending(); err != nil {
				log.Printf("[JSONStore] 异步刷盘失败: %v", err)
			}
		case <-ticker.C:
			if err := s.flushPending(); err != nil {
				log.Printf("[JSONStore] 定时刷盘失败: %v", err)
			}
		case <-s.stopCh:
			if err := s.flushPending(); err != nil {
				log.Printf("[JSONStore] 关闭前刷盘失败: %v", err)
			}
			return
		}
	}
}

// flushPending 刷新待落盘数据
func (s *JSONStore) flushPending() error {
	for {
		targetVersion := s.version.Load()
		persisted := s.persistedVersion.Load()
		if targetVersion <= persisted {
			return nil
		}

		snapshot := s.currentData()
		if err := s.persistToDisk(snapshot); err != nil {
			return err
		}

		s.markPersisted(targetVersion)
	}
}

// Save 同步保存数据
func (s *JSONStore) Save() error {
	s.mu.Lock()
	snapshot := s.currentData()
	targetVersion := s.version.Load()
	s.mu.Unlock()

	if err := s.persistToDisk(snapshot); err != nil {
		return err
	}
	s.markPersisted(targetVersion)
	return nil
}

// markPersisted 以单调递增方式更新已落盘版本
func (s *JSONStore) markPersisted(version uint64) {
	for {
		persisted := s.persistedVersion.Load()
		if version <= persisted {
			return
		}
		if s.persistedVersion.CompareAndSwap(persisted, version) {
			return
		}
	}
}

// mutate 执行 Copy-on-Write 更新
func (s *JSONStore) mutate(mutator func(data *AppData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return fmt.Errorf("存储已关闭")
	}

	current := s.currentData()
	next := cloneAppData(current)
	if err := mutator(next); err != nil {
		return err
	}

	s.data.Store(next)
	s.version.Add(1)
	s.notifySave()

	return nil
}

// notifySave 通知后台协程异步刷盘
func (s *JSONStore) notifySave() {
	select {
	case s.saveCh <- struct{}{}:
	default:
	}
}

// currentData 获取当前快照
func (s *JSONStore) currentData() *AppData {
	v := s.data.Load()
	if v == nil {
		return defaultAppData()
	}
	return v.(*AppData)
}

// ==================== 订阅操作 ====================

// GetSubscriptions 获取所有订阅
func (s *JSONStore) GetSubscriptions() []Subscription {
	snapshot := s.currentData()
	return cloneSubscriptions(snapshot.Subscriptions)
}

// GetSubscription 获取单个订阅
func (s *JSONStore) GetSubscription(id string) *Subscription {
	snapshot := s.currentData()

	for i := range snapshot.Subscriptions {
		if snapshot.Subscriptions[i].ID == id {
			sub := cloneSubscription(snapshot.Subscriptions[i])
			return &sub
		}
	}
	return nil
}

// AddSubscription 添加订阅
func (s *JSONStore) AddSubscription(sub Subscription) error {
	return s.mutate(func(data *AppData) error {
		data.Subscriptions = append(data.Subscriptions, sub)
		return nil
	})
}

// UpdateSubscription 更新订阅
func (s *JSONStore) UpdateSubscription(sub Subscription) error {
	activeNodeCount := 0
	for _, node := range sub.Nodes {
		if !node.Disabled {
			activeNodeCount++
		}
	}
	sub.NodeCount = activeNodeCount

	return s.mutate(func(data *AppData) error {
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID == sub.ID {
				data.Subscriptions[i] = sub
				return nil
			}
		}
		return fmt.Errorf("订阅不存在: %s", sub.ID)
	})
}

// SaveSubscriptionNodes 更新订阅的节点列表
func (s *JSONStore) SaveSubscriptionNodes(id string, nodes []Node) error {
	activeNodeCount := 0
	for _, node := range nodes {
		if !node.Disabled {
			activeNodeCount++
		}
	}

	return s.mutate(func(data *AppData) error {
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID == id {
				data.Subscriptions[i].Nodes = nodes
				data.Subscriptions[i].NodeCount = activeNodeCount
				return nil
			}
		}
		return fmt.Errorf("订阅不存在: %s", id)
	})
}

// DeleteSubscription 删除订阅
func (s *JSONStore) DeleteSubscription(id string) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID == id {
				data.Subscriptions = append(data.Subscriptions[:i], data.Subscriptions[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("订阅不存在: %s", id)
	})
}

// ==================== 过滤器操作 ====================

// GetFilters 获取所有过滤器
func (s *JSONStore) GetFilters() []Filter {
	snapshot := s.currentData()
	return cloneFilters(snapshot.Filters)
}

// GetFilter 获取单个过滤器
func (s *JSONStore) GetFilter(id string) *Filter {
	snapshot := s.currentData()

	for i := range snapshot.Filters {
		if snapshot.Filters[i].ID == id {
			filter := cloneFilter(snapshot.Filters[i])
			return &filter
		}
	}
	return nil
}

// AddFilter 添加过滤器
func (s *JSONStore) AddFilter(filter Filter) error {
	return s.mutate(func(data *AppData) error {
		data.Filters = append(data.Filters, filter)
		return nil
	})
}

// UpdateFilter 更新过滤器
func (s *JSONStore) UpdateFilter(filter Filter) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.Filters {
			if data.Filters[i].ID == filter.ID {
				data.Filters[i] = filter
				return nil
			}
		}
		return fmt.Errorf("过滤器不存在: %s", filter.ID)
	})
}

// DeleteFilter 删除过滤器
func (s *JSONStore) DeleteFilter(id string) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.Filters {
			if data.Filters[i].ID == id {
				data.Filters = append(data.Filters[:i], data.Filters[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("过滤器不存在: %s", id)
	})
}

// ==================== 规则操作 ====================

// GetRules 获取所有自定义规则
func (s *JSONStore) GetRules() []Rule {
	snapshot := s.currentData()
	return cloneRules(snapshot.Rules)
}

// AddRule 添加规则
func (s *JSONStore) AddRule(rule Rule) error {
	return s.mutate(func(data *AppData) error {
		data.Rules = append(data.Rules, rule)
		return nil
	})
}

// UpdateRule 更新规则
func (s *JSONStore) UpdateRule(rule Rule) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.Rules {
			if data.Rules[i].ID == rule.ID {
				data.Rules[i] = rule
				return nil
			}
		}
		return fmt.Errorf("规则不存在: %s", rule.ID)
	})
}

// DeleteRule 删除规则
func (s *JSONStore) DeleteRule(id string) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.Rules {
			if data.Rules[i].ID == id {
				data.Rules = append(data.Rules[:i], data.Rules[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("规则不存在: %s", id)
	})
}

// ==================== 规则组操作 ====================

// GetRuleGroups 获取所有预设规则组
func (s *JSONStore) GetRuleGroups() []RuleGroup {
	snapshot := s.currentData()
	return cloneRuleGroups(snapshot.RuleGroups)
}

// UpdateRuleGroup 更新规则组
func (s *JSONStore) UpdateRuleGroup(ruleGroup RuleGroup) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.RuleGroups {
			if data.RuleGroups[i].ID == ruleGroup.ID {
				data.RuleGroups[i] = ruleGroup
				return nil
			}
		}
		return fmt.Errorf("规则组不存在: %s", ruleGroup.ID)
	})
}

// ==================== 设置操作 ====================

// GetSettings 获取设置
func (s *JSONStore) GetSettings() *Settings {
	snapshot := s.currentData()
	return cloneSettings(snapshot.Settings)
}

// UpdateSettings 更新设置
func (s *JSONStore) UpdateSettings(settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("设置不能为空")
	}

	return s.mutate(func(data *AppData) error {
		data.Settings = cloneSettings(settings)
		return nil
	})
}

// ==================== 手动节点操作 ====================

// GetManualNodes 获取所有手动节点
func (s *JSONStore) GetManualNodes() []ManualNode {
	snapshot := s.currentData()
	return cloneManualNodes(snapshot.ManualNodes)
}

// AddManualNode 添加手动节点
func (s *JSONStore) AddManualNode(node ManualNode) error {
	return s.mutate(func(data *AppData) error {
		data.ManualNodes = append(data.ManualNodes, node)
		return nil
	})
}

// UpdateManualNode 更新手动节点
func (s *JSONStore) UpdateManualNode(node ManualNode) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.ManualNodes {
			if data.ManualNodes[i].ID == node.ID {
				data.ManualNodes[i] = node
				return nil
			}
		}
		return fmt.Errorf("手动节点不存在: %s", node.ID)
	})
}

// DeleteManualNode 删除手动节点
func (s *JSONStore) DeleteManualNode(id string) error {
	return s.mutate(func(data *AppData) error {
		for i := range data.ManualNodes {
			if data.ManualNodes[i].ID == id {
				data.ManualNodes = append(data.ManualNodes[:i], data.ManualNodes[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("手动节点不存在: %s", id)
	})
}

// ==================== 辅助方法 ====================

// GetAllNodes 获取所有启用的节点（订阅节点 + 手动节点）
func (s *JSONStore) GetAllNodes() []Node {
	snapshot := s.currentData()

	capacity := 0
	for _, sub := range snapshot.Subscriptions {
		if !sub.Enabled {
			continue
		}
		for _, node := range sub.Nodes {
			if !node.Disabled {
				capacity++
			}
		}
	}
	for _, mn := range snapshot.ManualNodes {
		if mn.Enabled {
			capacity++
		}
	}

	nodes := make([]Node, 0, capacity)

	for _, sub := range snapshot.Subscriptions {
		if !sub.Enabled {
			continue
		}
		for _, node := range sub.Nodes {
			if !node.Disabled {
				nodes = append(nodes, cloneNode(node))
			}
		}
	}

	for _, mn := range snapshot.ManualNodes {
		if mn.Enabled {
			nodes = append(nodes, cloneNode(mn.Node))
		}
	}

	return nodes
}

// GetAllNodesPtr 获取所有启用节点的指针切片（零拷贝优化）
// 返回的指针直接引用内部快照，调用者不应修改节点内容
func (s *JSONStore) GetAllNodesPtr() []*Node {
	snapshot := s.currentData()

	capacity := 0
	for _, sub := range snapshot.Subscriptions {
		if !sub.Enabled {
			continue
		}
		for _, node := range sub.Nodes {
			if !node.Disabled {
				capacity++
			}
		}
	}
	for _, mn := range snapshot.ManualNodes {
		if mn.Enabled {
			capacity++
		}
	}

	nodes := make([]*Node, 0, capacity)

	for i := range snapshot.Subscriptions {
		if !snapshot.Subscriptions[i].Enabled {
			continue
		}
		for j := range snapshot.Subscriptions[i].Nodes {
			if snapshot.Subscriptions[i].Nodes[j].Disabled {
				continue
			}
			nodes = append(nodes, &snapshot.Subscriptions[i].Nodes[j])
		}
	}

	for i := range snapshot.ManualNodes {
		if snapshot.ManualNodes[i].Enabled {
			nodes = append(nodes, &snapshot.ManualNodes[i].Node)
		}
	}

	return nodes
}

// GetNodesByCountry 按国家获取节点
func (s *JSONStore) GetNodesByCountry(countryCode string) []Node {
	snapshot := s.currentData()

	capacity := 0
	for _, sub := range snapshot.Subscriptions {
		if !sub.Enabled {
			continue
		}
		for _, node := range sub.Nodes {
			if !node.Disabled && node.Country == countryCode {
				capacity++
			}
		}
	}
	for _, mn := range snapshot.ManualNodes {
		if mn.Enabled && mn.Node.Country == countryCode {
			capacity++
		}
	}

	nodes := make([]Node, 0, capacity)

	for _, sub := range snapshot.Subscriptions {
		if !sub.Enabled {
			continue
		}
		for _, node := range sub.Nodes {
			if !node.Disabled && node.Country == countryCode {
				nodes = append(nodes, cloneNode(node))
			}
		}
	}

	for _, mn := range snapshot.ManualNodes {
		if mn.Enabled && mn.Node.Country == countryCode {
			nodes = append(nodes, cloneNode(mn.Node))
		}
	}

	return nodes
}

// GetCountryGroups 获取所有国家节点分组
func (s *JSONStore) GetCountryGroups() []CountryGroup {
	snapshot := s.currentData()

	countryCount := make(map[string]int)

	for _, sub := range snapshot.Subscriptions {
		if !sub.Enabled {
			continue
		}
		for _, node := range sub.Nodes {
			if !node.Disabled && node.Country != "" {
				countryCount[node.Country]++
			}
		}
	}

	for _, mn := range snapshot.ManualNodes {
		if mn.Enabled && mn.Node.Country != "" {
			countryCount[mn.Node.Country]++
		}
	}

	groups := make([]CountryGroup, 0, len(countryCount))
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

func defaultAppData() *AppData {
	return &AppData{
		Subscriptions: []Subscription{},
		ManualNodes:   []ManualNode{},
		Filters:       []Filter{},
		Rules:         []Rule{},
		RuleGroups:    DefaultRuleGroups(),
		Settings:      DefaultSettings(),
	}
}

func cloneAppData(src *AppData) *AppData {
	if src == nil {
		return defaultAppData()
	}

	return &AppData{
		Subscriptions: cloneSubscriptions(src.Subscriptions),
		ManualNodes:   cloneManualNodes(src.ManualNodes),
		Filters:       cloneFilters(src.Filters),
		Rules:         cloneRules(src.Rules),
		RuleGroups:    cloneRuleGroups(src.RuleGroups),
		Settings:      cloneSettings(src.Settings),
	}
}

func cloneSubscriptions(src []Subscription) []Subscription {
	if len(src) == 0 {
		return []Subscription{}
	}

	dst := make([]Subscription, len(src))
	for i := range src {
		dst[i] = cloneSubscription(src[i])
	}
	return dst
}

func cloneSubscription(src Subscription) Subscription {
	dst := src

	if src.ExpireAt != nil {
		expire := *src.ExpireAt
		dst.ExpireAt = &expire
	}

	if src.Traffic != nil {
		traffic := *src.Traffic
		dst.Traffic = &traffic
	}

	dst.Nodes = cloneNodes(src.Nodes)

	return dst
}

func cloneNodes(src []Node) []Node {
	if len(src) == 0 {
		return []Node{}
	}

	dst := make([]Node, len(src))
	for i := range src {
		dst[i] = cloneNode(src[i])
	}
	return dst
}

func cloneNode(src Node) Node {
	dst := src

	if src.Extra != nil {
		extra := make(map[string]interface{}, len(src.Extra))
		for key, value := range src.Extra {
			extra[key] = value
		}
		dst.Extra = extra
	}

	return dst
}

func cloneManualNodes(src []ManualNode) []ManualNode {
	if len(src) == 0 {
		return []ManualNode{}
	}

	dst := make([]ManualNode, len(src))
	for i := range src {
		dst[i] = src[i]
		dst[i].Node = cloneNode(src[i].Node)
	}
	return dst
}

func cloneFilters(src []Filter) []Filter {
	if len(src) == 0 {
		return []Filter{}
	}

	dst := make([]Filter, len(src))
	for i := range src {
		dst[i] = cloneFilter(src[i])
	}
	return dst
}

func cloneFilter(src Filter) Filter {
	dst := src
	dst.Subscriptions = append([]string(nil), src.Subscriptions...)
	dst.SelectedNodes = append([]string(nil), src.SelectedNodes...)

	if src.URLTestConfig != nil {
		cfg := *src.URLTestConfig
		dst.URLTestConfig = &cfg
	}

	return dst
}

func cloneRules(src []Rule) []Rule {
	if len(src) == 0 {
		return []Rule{}
	}

	dst := make([]Rule, len(src))
	for i := range src {
		dst[i] = src[i]
		dst[i].Values = append([]string(nil), src[i].Values...)
	}
	return dst
}

func cloneRuleGroups(src []RuleGroup) []RuleGroup {
	if len(src) == 0 {
		return []RuleGroup{}
	}

	dst := make([]RuleGroup, len(src))
	for i := range src {
		dst[i] = src[i]
		dst[i].SiteRules = append([]string(nil), src[i].SiteRules...)
		dst[i].IPRules = append([]string(nil), src[i].IPRules...)
	}
	return dst
}

func cloneSettings(src *Settings) *Settings {
	if src == nil {
		return DefaultSettings()
	}

	dst := *src
	dst.Hosts = cloneHostEntries(src.Hosts)

	return &dst
}

func cloneHostEntries(src []HostEntry) []HostEntry {
	if len(src) == 0 {
		return []HostEntry{}
	}

	dst := make([]HostEntry, len(src))
	for i := range src {
		dst[i] = src[i]
		dst[i].IPs = append([]string(nil), src[i].IPs...)
	}
	return dst
}
