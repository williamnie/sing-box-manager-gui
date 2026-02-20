# Spec: 存储 I/O 优化

## ADDED Requirements

### Requirement: Storage MUST buffer writes and batch commits

**ID:** REQ-SIO-001

**Priority:** HIGH

**Description:**
MUST 减少磁盘 I/O 次数,将频繁的小写入合并为批量写入。

#### Scenario: 快速连续更新

**Given:** 用户在 10 秒内修改 5 个规则

**When:** 每次修改都调用 `UpdateRule`

**Then:**
- 仅触发 1-2 次实际磁盘写入
- 所有更新都正确持久化
- 最后一次更新后 1 秒内完成写入

**Acceptance Criteria:**
- 写入请求进入缓冲队列
- 后台协程定期刷盘(500ms 或收到信号)
- 应用退出时强制刷盘

---

### Requirement: Generated configuration SHALL be cached

**ID:** REQ-SIO-002

**Priority:** HIGH

**Description:**
SHALL 缓存生成的 sing-box 配置,避免重复序列化和构建。

#### Scenario: 频繁预览配置

**Given:** 用户多次点击"预览配置"按钮

**When:** 数据未发生变更

**Then:**
- 直接返回缓存的配置文件
- 响应时间 < 5ms
- 不重新执行配置构建逻辑

**Acceptance Criteria:**
- 使用数据版本号标识配置是否过期
- 版本号在任何 CRUD 操作时递增
- 缓存使用读写锁保护

---

### Requirement: JSON serialization SHALL be optimized

**ID:** REQ-SIO-003

**Priority:** MEDIUM

**Description:**
SHALL 使用更高效的 JSON 库,减少 CPU 占用。

#### Scenario: 大数据序列化

**Given:** `data.json` 包含 1000+ 节点数据(~500KB)

**When:** 执行 `saveInternal()`

**Then:**
- 序列化时间 < 20ms
- CPU 占用 < 10%
- 内存增长 < 5MB

**Acceptance Criteria:**
- 考虑使用 `github.com/json-iterator/go`
- 或保持标准库但优化数据结构
- 基准测试验证性能提升
