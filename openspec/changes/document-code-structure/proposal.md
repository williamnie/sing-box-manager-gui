# Proposal: Document Code Structure

## Change ID
`document-code-structure`

## Status
Implemented

## Summary
梳理并文档化项目的代码结构，将完整的架构说明和目录组织方式添加到 README.md 文件中，帮助开发者快速理解项目布局和各模块职责。

## Problem Statement
当前 README.md 主要介绍了项目的功能特性、安装和使用方法，但缺少对代码结构的详细说明。新的开发者或贡献者需要花费大量时间探索代码库才能理解项目的组织方式和各模块的职责。

## Goals
1. 在 README.md 中添加完整的项目结构说明
2. 描述后端（Go）和前端（React/TypeScript）的目录组织
3. 说明各模块的职责和相互关系
4. 提供技术架构概览，帮助开发者快速上手

## Non-Goals
- 不修改代码实现或重构项目结构
- 不添加 API 文档（这应该是单独的文档）
- 不详细描述每个文件的具体实现细节

## Proposed Solution

### 文档结构
在 README.md 的技术栈部分之后添加新的章节：

1. **项目结构（Project Structure）**
   - 整体目录树视图
   - 各目录的简要说明

2. **后端架构（Backend Architecture）**
   - cmd/: 应用程序入口
   - internal/: 核心业务逻辑
     - api/: HTTP API 路由和处理器
     - parser/: 订阅格式解析器（支持多种协议）
     - storage/: 数据存储层
     - daemon/: sing-box 进程管理
     - service/: 业务服务层
     - kernel/: sing-box 内核管理
     - builder/: sing-box 配置生成器
     - logger/: 日志管理
   - pkg/: 可复用的工具包

3. **前端架构（Frontend Architecture）**
   - web/src/pages/: 页面组件
   - web/src/components/: 共享组件
   - web/src/api/: API 客户端
   - web/src/store/: 状态管理（Zustand）

### 架构原则
- **单一职责**: 每个模块专注于特定功能
- **清晰分层**: API → Service → Storage 的清晰分层
- **协议解耦**: 各协议解析器独立实现，易于扩展

## Implementation Plan
详见 `tasks.md`

## Testing Strategy
- 人工审查文档的准确性和完整性
- 确保文档与实际代码结构一致
- 由团队成员验证文档的可读性和实用性

## Rollout Plan
1. 创建提案并获得批准
2. 编写文档内容
3. 代码审查
4. 合并到主分支

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| 文档可能很快过时 | 在重构或添加新模块时同步更新 README |
| 过于详细导致维护困难 | 保持高层次概览，避免深入实现细节 |
| 中英双语可能不一致 | 确保两种语言描述相同的内容 |

## Open Questions
- [ ] 是否需要添加架构图（如使用 Mermaid）？
- [ ] 是否需要说明数据流向？
- [ ] 文档应该多详细（目录级别 vs 文件级别）？

## References
- 现有 README.md: `/Users/Zhuanz/mini/sing-box/sing-box-manager-gui/README.md`
- 项目源代码: `/Users/Zhuanz/mini/sing-box/sing-box-manager-gui/`
