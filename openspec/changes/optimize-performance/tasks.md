# Tasks: 性能优化实施清单

## 阶段 1: 快速优化(低风险,高收益)

### 前端快速优化
- [x] **T1.1**: 实现 Page Visibility API 控制轮询
  - 文件: `web/src/pages/Dashboard.tsx:38-68`
  - 添加 `visibilitychange` 事件监听
  - 页面隐藏时暂停定时器
  - 页面可见时立即刷新
  - ✅ 已完成

- [x] **T1.2**: Dashboard 使用 Zustand 选择器
  - 文件: `web/src/pages/Dashboard.tsx:8-15`
  - 替换 `useStore()` 为 `useStore(state => state.xxx)`
  - 优化渲染性能,避免无关状态更新触发重渲染
  - ✅ 已完成

- [ ] **T1.3**: 订阅卡片添加 React.memo
  - 文件: `web/src/pages/Subscriptions.tsx`
  - 提取订阅卡片为独立组件
  - 使用 `React.memo` 包裹
  - 估时: 1h

- [ ] **T1.4**: 格式化函数使用 useMemo
  - 文件: `web/src/pages/*.tsx`
  - 日期格式化、流量转换等使用 `useMemo`
  - 估时: 1h

### 后端快速优化
- [x] **T1.5**: HTTP 客户端单例化
  - 文件: `pkg/utils/httpclient.go` (新建)
  - 文件: `pkg/utils/http.go:23` (更新)
  - 文件: `internal/api/router.go:539-542` (更新)
  - 使用 `sync.Once` 创建全局客户端
  - 配置连接池: MaxIdleConns=100, MaxIdleConnsPerHost=10
  - ✅ 已完成

- [x] **T1.6**: 进程监控缓存
  - 文件: `internal/api/router.go:1164-1238`
  - 添加缓存结构体 `cachedSystemInfo`
  - 2秒内复用监控数据,减少系统调用
  - ✅ 已完成

**阶段 1 总估时: 8 小时**

---

## 阶段 2: 中等复杂度优化

### 前端渲染优化
- [ ] **T2.1**: 安装并集成 react-window
  - 依赖: `npm install react-window`
  - 估时: 0.5h

- [ ] **T2.2**: 节点列表虚拟化
  - 文件: `web/src/pages/Subscriptions.tsx`
  - 使用 `FixedSizeList` 替换原始列表
  - 处理选中状态和操作按钮
  - 估时: 4h

- [ ] **T2.3**: 搜索输入防抖
  - 文件: `web/src/pages/Subscriptions.tsx`, `Rules.tsx`
  - 添加 debounce hook
  - 应用于搜索框
  - 估时: 2h

### 后端并发优化
- [x] **T2.4**: 并发订阅刷新
  - 文件: `internal/service/subscription.go:84-117`
  - 使用 goroutine + WaitGroup 并发刷新
  - 添加 semaphore 限制并发数为 5
  - 错误隔离,单个失败不影响其他订阅
  - ✅ 已完成 - 预计提升 3-5倍刷新速度

- [x] **T2.5**: 异步配置应用
  - 文件: `internal/api/router.go:56-57,81-89,724-785`
  - 创建配置应用队列 `configQueue` (缓冲区=1)
  - 后台 worker goroutine `configApplyWorker`
  - 自动去重逻辑(队列满时跳过)
  - 添加 `Shutdown()` 优雅关闭方法
  - ✅ 已完成 - API 响应不再阻塞

- [ ] **T2.6**: 配置缓存机制
  - 文件: `internal/api/router.go`, `internal/storage/json_store.go`
  - 添加数据版本号(atomic.Uint64)
  - 实现缓存结构
  - CRUD 操作自动失效缓存
  - 估时: 3h

**阶段 2 总估时: 16.5 小时**

---

## 阶段 3: 高收益优化(高复杂度)

### 存储层优化
- [ ] **T3.1**: Copy-on-Write 存储模式
  - 文件: `internal/storage/json_store.go`
  - 使用 `atomic.Value` 存储数据指针
  - 实现数据浅拷贝函数
  - 更新所有读写方法
  - 估时: 6h

- [ ] **T3.2**: 写入缓冲队列
  - 文件: `internal/storage/json_store.go`
  - 添加保存通道和后台协程
  - 实现定时刷盘逻辑
  - 应用退出时强制刷盘
  - 估时: 4h

- [ ] **T3.3**: 原子写入保护
  - 文件: `internal/storage/json_store.go`
  - 写入临时文件后 rename
  - 备份机制
  - 估时: 2h

### 性能测试与验证
- [ ] **T3.4**: 编写性能基准测试
  - 文件: `internal/storage/*_test.go`, `internal/api/*_test.go`
  - 并发读写基准
  - 内存分配测试
  - race detector 验证
  - 估时: 4h

- [ ] **T3.5**: 前端性能测试
  - 使用 Lighthouse 测试
  - React DevTools Profiler 分析
  - 网络请求监控
  - 估时: 2h

- [ ] **T3.6**: 压力测试
  - Apache Bench 并发测试
  - 1000+ 节点数据加载
  - 24h 稳定性测试
  - 估时: 4h

**阶段 3 总估时: 22 小时**

---

## 阶段 4: 文档与收尾

- [ ] **T4.1**: 更新性能文档
  - 优化前后对比数据
  - 配置建议
  - 估时: 2h

- [ ] **T4.2**: 添加特性开关
  - 环境变量控制优化开关
  - 前端 feature flags
  - 估时: 2h

- [ ] **T4.3**: Code Review 与修正
  - 估时: 4h

**阶段 4 总估时: 8 小时**

---

## 总计估时: 54.5 小时 (~7 工作日)

## 依赖关系

```
T1.x (并行) → T2.x (部分并行) → T3.x (顺序) → T4.x

关键路径:
T1.5 → T2.4 → T3.1 → T3.2 → T3.4
```

## 验证检查清单

每个任务完成后必须检查:
- [ ] 功能正常,无回归
- [ ] 性能指标达标
- [ ] 通过 race detector
- [ ] 代码 review 通过
- [ ] 文档已更新

## 回滚策略

每个阶段完成后创建 git tag:
- `perf-stage1`
- `perf-stage2`
- `perf-stage3`

如发现问题可快速回退到上一阶段。
