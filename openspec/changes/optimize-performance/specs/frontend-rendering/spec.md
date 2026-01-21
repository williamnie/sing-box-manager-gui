# Spec: 前端渲染优化

## ADDED Requirements

### Requirement: Dashboard SHALL implement智能轮询控制

**ID:** REQ-FR-001

**Priority:** HIGH

**Description:**
Dashboard 和其他页面 SHALL 仅在用户可见时执行数据轮询,避免浪费资源。

#### Scenario: 页面隐藏时暂停轮询

**Given:** Dashboard 页面正在轮询系统状态(每5秒)

**When:** 用户切换到其他标签页或最小化浏览器

**Then:**
- 立即暂停所有轮询定时器
- 不再发送 API 请求
- 当用户返回页面时,立即执行一次刷新并恢复轮询

**Acceptance Criteria:**
- 使用 `document.hidden` 和 `visibilitychange` 事件
- 页面隐藏超过 30 秒后首次返回应刷新数据
- 网络请求数量减少 50% 以上(典型多标签使用场景)

---

### Requirement: Components MUST optimize rendering

**ID:** REQ-FR-002

**Priority:** HIGH

**Description:**
组件 MUST 减少不必要的重渲染,提升交互响应速度。

#### Scenario: 状态更新仅影响相关组件

**Given:** 用户在 Dashboard 页面

**When:** 系统监控信息更新(CPU/内存数据)

**Then:**
- 仅资源监控卡片重新渲染
- 服务状态卡片、订阅列表卡片不重渲染
- 整体渲染时间 < 16ms (保持 60 FPS)

**Acceptance Criteria:**
- 所有组件使用 Zustand 状态选择器
- 列表项组件包裹 `React.memo`
- 昂贵计算使用 `useMemo`
- 事件处理函数使用 `useCallback`

#### Scenario: 长列表流畅滚动

**Given:** 订阅包含 500+ 节点

**When:** 用户在节点列表页滚动

**Then:**
- 滚动帧率保持 60 FPS
- 初始渲染时间 < 300ms
- 内存占用增长 < 20MB

**Acceptance Criteria:**
- 使用虚拟滚动库(react-window 或 react-virtualized)
- 可见区域外节点不渲染 DOM
- 支持动态行高(可选)

---

### Requirement: Frequent operations SHALL apply debounce/throttle

**ID:** REQ-FR-003

**Priority:** MEDIUM

**Description:**
频繁触发的操作 SHALL 应用防抖或节流,减少后端压力和前端计算。

#### Scenario: 搜索输入防抖

**Given:** 用户在节点列表页使用搜索框

**When:** 用户快速输入 "hongkong"

**Then:**
- 仅在输入停止 300ms 后执行过滤
- 输入过程中不触发过滤计算
- 避免每次按键都重新渲染列表

**Acceptance Criteria:**
- 使用 lodash.debounce 或自定义 hook
- 防抖延迟 300ms
- 支持立即取消(用户清空输入)

---

### Requirement: Large datasets SHALL load asynchronously

**ID:** REQ-FR-004

**Priority:** MEDIUM

**Description:**
大数据集 SHALL 分批加载,避免阻塞 UI 线程。

#### Scenario: 分页加载节点

**Given:** 用户打开包含 1000+ 节点的订阅

**When:** 页面首次加载

**Then:**
- 首屏显示前 50 个节点(< 200ms)
- 后续节点在后台异步加载
- 显示加载进度指示器

**Acceptance Criteria:**
- 使用 Intersection Observer API 实现懒加载
- 或使用虚拟滚动自动处理
- 加载状态可视化

---

## Performance Targets

| 指标 | 当前 | 目标 | 测量方法 |
|-----|------|------|---------|
| Dashboard 轮询请求数 | 12 req/min | 6 req/min | Chrome DevTools Network |
| 节点列表渲染时间 (500节点) | ~800ms | <300ms | React DevTools Profiler |
| 滚动帧率 (1000节点) | 30-40 FPS | 60 FPS | Chrome Performance |
| Bundle 大小增长 | - | <50KB | webpack-bundle-analyzer |

## Related Capabilities

- **backend-concurrency**: 后端并发优化减少 API 响应时间
- **resource-management**: 降低客户端内存占用
