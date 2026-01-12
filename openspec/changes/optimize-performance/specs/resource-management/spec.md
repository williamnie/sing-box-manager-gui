# Spec: 资源管理优化

## ADDED Requirements

### Requirement: Object pools SHALL be used for high-frequency allocations

**ID:** REQ-RM-001

**Priority:** MEDIUM

**Description:**
SHALL 使用对象池减少内存分配和 GC 压力。

#### Scenario: 节点数据复用

**Given:** 系统频繁解析和处理节点数据

**When:** 解析 100 个节点

**Then:**
- 使用 `sync.Pool` 复用 Node 对象
- 减少 50% 的内存分配
- GC 暂停时间降低

**Acceptance Criteria:**
- 为高频对象(Node, Outbound)创建对象池
- 使用后正确归还对象
- 重置对象状态避免污染

---

### Requirement: Process metrics SHALL be cached

**ID:** REQ-RM-002

**Priority:** MEDIUM

**Description:**
SHALL 缓存进程资源统计,减少系统调用频率。

#### Scenario: Dashboard 轮询监控数据

**Given:** Dashboard 每 5 秒请求系统监控数据

**When:** `/api/monitor/system` 被调用

**Then:**
- 如果缓存未过期(< 2s),直接返回缓存
- 避免每次调用 `gopsutil`
- CPU 使用率降低

**Acceptance Criteria:**
- 缓存有效期 2 秒
- 使用读写锁保护缓存
- 缓存过期时才调用系统 API

---

### Requirement: Goroutines MUST have proper lifecycle management

**ID:** REQ-RM-003

**Priority:** MEDIUM

**Description:**
MUST 正确管理 goroutine,避免泄漏和资源浪费。

#### Scenario: 服务关闭时清理

**Given:** 应用启动后创建多个后台 goroutine

**When:** 接收到退出信号

**Then:**
- 所有 goroutine 收到停止信号
- 在 5 秒内优雅退出
- 不遗留僵尸进程

**Acceptance Criteria:**
- 使用 `context.Context` 传递取消信号
- 等待所有 goroutine 结束(WaitGroup)
- 超时强制退出
