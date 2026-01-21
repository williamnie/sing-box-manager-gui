# Spec: 后端并发优化

## ADDED Requirements

### Requirement: Storage layer SHALL support fine-grained concurrency

**ID:** REQ-BC-001

**Priority:** HIGH

**Description:**
存储层 SHALL 支持高并发读写,减少锁竞争,提升吞吐量。

#### Scenario: 并发读取不阻塞

**Given:** 100 个并发 API 请求读取订阅列表

**When:** 同时有 1 个请求正在更新订阅

**Then:**
- 所有读请求无需等待写锁
- 读操作延迟 < 5ms
- 写操作完成时间 < 50ms

**Acceptance Criteria:**
- 使用 `atomic.Value` + Copy-on-Write 模式
- 读操作无锁或使用读锁
- 写操作仅在数据更新时短暂加锁

---

### Requirement: Configuration MUST be applied asynchronously

**ID:** REQ-BC-002

**Priority:** HIGH

**Description:**
配置应用 MUST NOT 阻塞 API 响应,用户操作应立即返回。

#### Scenario: 添加订阅立即返回

**Given:** 用户通过 API 添加新订阅

**When:** 订阅数据保存成功

**Then:**
- API 立即返回 200 OK (< 100ms)
- 配置生成和应用在后台异步执行
- 前端可通过轮询获取应用状态

**Acceptance Criteria:**
- 使用 goroutine + channel 实现任务队列
- 配置应用失败时记录日志并通知用户
- 同一时间仅处理一个配置应用任务(去重)

---

### Requirement: Subscriptions SHALL refresh concurrently

**ID:** REQ-BC-003

**Priority:** MEDIUM

**Description:**
多个订阅 SHALL 并发刷新,提升整体速度。

#### Scenario: 刷新所有订阅

**Given:** 用户有 10 个启用的订阅

**When:** 执行"刷新所有订阅"操作

**Then:**
- 最多 5 个订阅并发拉取
- 总耗时接近最慢订阅的时间(而非累加)
- 单个订阅失败不影响其他订阅

**Acceptance Criteria:**
- 使用 goroutine + WaitGroup
- 通过 semaphore 限制并发数(默认 5)
- 错误隔离,不传播
