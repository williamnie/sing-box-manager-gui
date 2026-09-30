package service

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobei/singbox-manager/internal/parser"
	"github.com/xiaobei/singbox-manager/internal/storage"
	"github.com/xiaobei/singbox-manager/pkg/utils"
)

// SubscriptionService 订阅服务
type SubscriptionService struct {
	store *storage.JSONStore
}

// NewSubscriptionService 创建订阅服务
func NewSubscriptionService(store *storage.JSONStore) *SubscriptionService {
	return &SubscriptionService{
		store: store,
	}
}

// GetAll 获取所有订阅
func (s *SubscriptionService) GetAll() []storage.Subscription {
	return s.store.GetSubscriptions()
}

// Get 获取单个订阅
func (s *SubscriptionService) Get(id string) *storage.Subscription {
	return s.store.GetSubscription(id)
}

// Add 添加订阅
func (s *SubscriptionService) Add(name, url string) (*storage.Subscription, error) {
	sub := storage.Subscription{
		ID:        uuid.New().String(),
		Name:      name,
		URL:       url,
		NodeCount: 0,
		UpdatedAt: time.Now(),
		Nodes:     []storage.Node{},
		Enabled:   true,
	}

	// 拉取并解析订阅
	if err := s.refresh(&sub); err != nil {
		return nil, fmt.Errorf("拉取订阅失败: %w", err)
	}

	// 保存订阅
	if err := s.store.AddSubscription(sub); err != nil {
		return nil, fmt.Errorf("保存订阅失败: %w", err)
	}

	return &sub, nil
}

// Update 更新订阅
func (s *SubscriptionService) Update(sub storage.Subscription) error {
	return s.store.UpdateSubscription(sub)
}

// Delete 删除订阅
func (s *SubscriptionService) Delete(id string) error {
	return s.store.DeleteSubscription(id)
}

// Refresh 刷新订阅
func (s *SubscriptionService) Refresh(id string) error {
	sub := s.store.GetSubscription(id)
	if sub == nil {
		return fmt.Errorf("订阅不存在: %s", id)
	}

	if err := s.refresh(sub); err != nil {
		return err
	}

	return s.store.UpdateSubscription(*sub)
}

// RefreshAll 刷新所有订阅
func (s *SubscriptionService) RefreshAll() error {
	subs := s.store.GetSubscriptions()
	failures := 0
	for _, sub := range subs {
		if sub.Enabled {
			if err := s.refresh(&sub); err != nil {
				// 记录失败数量，不把含凭据的订阅 URL 写入日志。
				failures++
				continue
			}
			if err := s.store.UpdateSubscription(sub); err != nil {
				failures++
				continue
			}
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d 个订阅更新失败，请检查订阅配置", failures)
	}
	return nil
}

// refresh 内部刷新方法
func (s *SubscriptionService) refresh(sub *storage.Subscription) error {
	// 拉取订阅内容
	content, info, err := utils.FetchSubscription(sub.URL)
	if err != nil {
		return fmt.Errorf("拉取订阅失败: %w", err)
	}

	// 解析节点
	nodes, err := parser.ParseSubscriptionContent(content)
	if err != nil {
		return fmt.Errorf("解析订阅失败: %w", err)
	}

	// 更新订阅信息
	sub.Nodes = nodes
	sub.NodeCount = len(nodes)
	sub.UpdatedAt = time.Now()

	// 更新流量信息
	if info != nil && info.Total > 0 {
		sub.Traffic = &storage.Traffic{
			Total:     info.Total,
			Used:      info.Upload + info.Download,
			Remaining: info.Total - info.Upload - info.Download,
		}
		sub.ExpireAt = info.Expire
	}

	return nil
}

// Toggle 切换订阅启用状态
func (s *SubscriptionService) Toggle(id string, enabled bool) error {
	sub := s.store.GetSubscription(id)
	if sub == nil {
		return fmt.Errorf("订阅不存在: %s", id)
	}

	sub.Enabled = enabled
	return s.store.UpdateSubscription(*sub)
}
