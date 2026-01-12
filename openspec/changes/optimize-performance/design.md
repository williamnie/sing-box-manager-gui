# Design: 性能优化架构设计

## 架构原则

### 1. 渐进式优化
每个优化点都可独立实现,不影响其他模块

### 2. 零破坏性
保持 100% 向后兼容,不改变现有 API 契约

### 3. 可度量性
所有优化都有明确的性能指标和测量方法

## 核心优化设计

### 前端渲染优化

#### 问题分析
```typescript
// 当前问题: Dashboard.tsx
useEffect(() => {
  fetchServiceStatus();
  fetchSystemInfo();
  const interval = setInterval(() => {
    fetchServiceStatus();  // 每5秒无条件轮询
    fetchSystemInfo();
  }, 5000);
  return () => clearInterval(interval);
}, []);
```

**问题点:**
- 页面不可见时仍然轮询
- 所有组件共享同一个 store,任何更新都触发全局重渲染
- 无防抖/节流机制

#### 解决方案

**1. Page Visibility API**
```typescript
useEffect(() => {
  const poll = () => {
    if (!document.hidden) {  // 仅在页面可见时轮询
      fetchServiceStatus();
      fetchSystemInfo();
    }
  };

  const interval = setInterval(poll, 5000);
  document.addEventListener('visibilitychange', poll);

  return () => {
    clearInterval(interval);
    document.removeEventListener('visibilitychange', poll);
  };
}, []);
```

**2. Zustand 选择器优化**
```typescript
// 当前: 组件订阅整个 store
const { subscriptions, systemInfo } = useStore();

// 优化: 仅订阅需要的状态
const subscriptions = useStore(state => state.subscriptions);
const systemInfo = useStore(state => state.systemInfo);
```

**3. React.memo + useMemo**
```typescript
const SubscriptionCard = React.memo(({ subscription }) => {
  const formattedDate = useMemo(
    () => new Date(subscription.updated_at).toLocaleString(),
    [subscription.updated_at]
  );
  return <Card>...</Card>;
});
```

**4. 虚拟列表**
```typescript
import { FixedSizeList } from 'react-window';

// 替换长列表渲染
<FixedSizeList
  height={600}
  itemCount={nodes.length}
  itemSize={80}
>
  {({ index, style }) => (
    <NodeItem node={nodes[index]} style={style} />
  )}
</FixedSizeList>
```

### 后端并发优化

#### 问题分析
```go
// internal/storage/json_store.go
func (s *JSONStore) UpdateSubscription(sub Subscription) error {
    s.mu.Lock()           // 阻塞所有并发读写
    defer s.mu.Unlock()

    // ... 更新逻辑
    return s.saveInternal()  // 每次都写磁盘
}

// internal/api/router.go
func (s *Server) addSubscription(c *gin.Context) {
    // ...
    if err := s.autoApplyConfig(); err != nil {  // 同步阻塞
        // ...
    }
}
```

**问题点:**
- 粗粒度全局锁
- 同步磁盘 I/O
- 串行订阅刷新
- 阻塞式配置应用

#### 解决方案

**1. 细粒度锁 + Copy-on-Write**
```go
type JSONStore struct {
    dataDir string
    mu      sync.RWMutex
    data    atomic.Value  // 存储 *AppData 指针

    // 新增: 写入缓冲通道
    saveCh  chan struct{}
    done    chan struct{}
}

// 读操作无需加锁
func (s *JSONStore) GetSubscriptions() []Subscription {
    data := s.data.Load().(*AppData)
    return data.Subscriptions
}

// 写操作使用 COW
func (s *JSONStore) UpdateSubscription(sub Subscription) error {
    s.mu.Lock()
    oldData := s.data.Load().(*AppData)
    newData := s.copyAppData(oldData)  // 浅拷贝

    // 更新数据
    for i := range newData.Subscriptions {
        if newData.Subscriptions[i].ID == sub.ID {
            newData.Subscriptions[i] = sub
            break
        }
    }

    s.data.Store(newData)
    s.mu.Unlock()

    // 异步触发保存
    select {
    case s.saveCh <- struct{}{}:
    default:
    }

    return nil
}

// 后台写入协程
func (s *JSONStore) saveWorker() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-s.saveCh:
        case <-ticker.C:
        case <-s.done:
            return
        }

        data := s.data.Load().(*AppData)
        s.persistToDisk(data)
    }
}
```

**2. 异步配置应用**
```go
type Server struct {
    // ... 现有字段
    configQueue chan struct{}
}

func (s *Server) autoApplyConfig() error {
    if !settings.AutoApply {
        return nil
    }

    // 非阻塞发送
    select {
    case s.configQueue <- struct{}{}:
    default:
        // 已有待处理任务,跳过
    }

    return nil
}

func (s *Server) configApplyWorker() {
    for range s.configQueue {
        configJSON, _ := s.buildConfig()
        s.saveConfigFile(...)
        if s.processManager.IsRunning() {
            s.processManager.Restart()
        }
    }
}
```

**3. 并发订阅刷新**
```go
func (s *SubscriptionService) RefreshAll() error {
    subs := s.store.GetSubscriptions()

    var wg sync.WaitGroup
    semaphore := make(chan struct{}, 5)  // 限制并发数

    for _, sub := range subs {
        if !sub.Enabled {
            continue
        }

        wg.Add(1)
        go func(sub storage.Subscription) {
            defer wg.Done()

            semaphore <- struct{}{}
            defer func() { <-semaphore }()

            s.refresh(&sub)
            s.store.UpdateSubscription(sub)
        }(sub)
    }

    wg.Wait()
    return nil
}
```

### 存储 I/O 优化

#### 问题分析
- 每次数据变更都完整序列化 + 写入磁盘
- 无缓存机制,配置构建重复计算
- JSON 序列化性能随数据规模下降

#### 解决方案

**1. 写入缓冲与合并**
```go
type bufferedStore struct {
    *JSONStore
    lastSave   time.Time
    dirty      bool
    saveMu     sync.Mutex
}

func (s *bufferedStore) scheduleSave() {
    s.saveMu.Lock()
    defer s.saveMu.Unlock()

    if time.Since(s.lastSave) < 500*time.Millisecond {
        // 短时间内多次写入,延迟保存
        s.dirty = true
        return
    }

    s.persistToDisk()
    s.dirty = false
    s.lastSave = time.Now()
}
```

**2. 配置缓存**
```go
type ConfigCache struct {
    mu      sync.RWMutex
    cache   string
    version uint64
}

func (s *Server) buildConfig() (string, error) {
    version := s.store.GetVersion()  // 数据版本号

    s.configCache.mu.RLock()
    if s.configCache.version == version {
        cached := s.configCache.cache
        s.configCache.mu.RUnlock()
        return cached, nil
    }
    s.configCache.mu.RUnlock()

    // 重新构建
    config, err := s.actuallyBuildConfig()

    s.configCache.mu.Lock()
    s.configCache.cache = config
    s.configCache.version = version
    s.configCache.mu.Unlock()

    return config, err
}
```

**3. HTTP 客户端复用**
```go
var (
    httpClient     *http.Client
    httpClientOnce sync.Once
)

func getHTTPClient() *http.Client {
    httpClientOnce.Do(func() {
        httpClient = &http.Client{
            Timeout: 30 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
            },
        }
    })
    return httpClient
}
```

### 资源管理优化

#### 内存优化
```go
// 避免节点数据重复复制
type nodeRef struct {
    *storage.Node
}

// 对象池
var nodePool = sync.Pool{
    New: func() interface{} {
        return &storage.Node{}
    },
}
```

#### 监控优化
```go
// 缓存进程信息,避免频繁系统调用
type cachedProcessInfo struct {
    stats     ProcessStats
    timestamp time.Time
    mu        sync.RWMutex
}

func (c *cachedProcessInfo) get() ProcessStats {
    c.mu.RLock()
    if time.Since(c.timestamp) < 2*time.Second {
        stats := c.stats
        c.mu.RUnlock()
        return stats
    }
    c.mu.RUnlock()

    // 刷新缓存
    newStats := fetchFromSystem()
    c.mu.Lock()
    c.stats = newStats
    c.timestamp = time.Now()
    c.mu.Unlock()

    return newStats
}
```

## 性能指标

### 前端
- **首次渲染时间 (FCP)**: < 1s
- **交互响应延迟 (TTI)**: < 100ms
- **列表滚动帧率**: 60 FPS (1000+ 节点)
- **轮询数据传输**: 减少 60%

### 后端
- **API 平均响应**: < 50ms
- **订阅刷新速度**: 提升 3-5x
- **并发请求吞吐**: +200%
- **内存占用**: -30%

### 存储
- **磁盘写入次数**: -70%
- **配置生成时间**: -80% (缓存命中)
- **锁竞争等待**: -90%

## 测试策略

### 单元测试
- 并发安全性测试(race detector)
- 缓存一致性验证
- 内存泄漏检测

### 性能基准
```go
func BenchmarkStoreUpdate(b *testing.B) {
    store := setupTestStore()
    b.ResetTimer()

    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            store.UpdateSubscription(testSubs[i%len(testSubs)])
            i++
        }
    })
}
```

### 压力测试
- 100+ 并发 API 请求
- 1000+ 节点数据加载
- 长时间运行稳定性(24h)

## 实施顺序

**阶段 1: 低风险快速优化** (1-2 天)
1. 前端选择器优化
2. Page Visibility API
3. HTTP 客户端复用

**阶段 2: 中等复杂度优化** (3-5 天)
1. 写入缓冲
2. 配置缓存
3. 并发订阅刷新

**阶段 3: 高收益优化** (5-7 天)
1. 虚拟列表渲染
2. COW 存储模式
3. 异步配置应用

## 回退机制

所有优化通过环境变量或配置控制:
```go
const (
    EnableAsyncConfig = true  // 可配置
    EnableConfigCache = true
    ConcurrentRefresh = 5     // 并发数
)
```

前端使用特性开关:
```typescript
const FEATURES = {
  virtualList: true,
  optimizedPolling: true,
};
```
