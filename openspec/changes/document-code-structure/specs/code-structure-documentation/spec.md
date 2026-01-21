# Spec: Code Structure Documentation

## ADDED Requirements

### Requirement: Project Structure Overview
README.md MUST include a project structure overview section that displays a complete directory tree and functional descriptions of each directory.

#### Scenario: 开发者查看项目结构
**Given** 一个新的开发者或贡献者查看 README.md
**When** 他们阅读项目结构章节
**Then** 他们应该能够：
- 看到完整的项目目录树（排除 node_modules、.git 等）
- 理解每个顶级目录的用途
- 快速定位到感兴趣的模块

---

### Requirement: Backend Architecture Documentation
README.md MUST provide detailed documentation of the backend code architecture organization, including the responsibilities of cmd, internal, and pkg directories.

#### Scenario: 开发者了解后端模块
**Given** 开发者需要修改或扩展后端功能
**When** 他们查看后端架构文档
**Then** 他们应该能够：
- 理解应用程序入口点（cmd/sbm）
- 找到核心业务逻辑所在（internal/）
- 了解各子模块的职责：
  - api/: HTTP API 路由和处理器
  - parser/: 各协议解析器（SS、VMess、VLESS、Trojan、Hysteria2、TUIC）
  - storage/: 数据存储和模型定义
  - daemon/: sing-box 进程生命周期管理
  - service/: 业务服务层（订阅、调度等）
  - kernel/: sing-box 内核管理
  - builder/: sing-box 配置文件生成
  - logger/: 日志系统
- 识别可复用的工具包（pkg/utils）

#### Scenario: 开发者添加新的协议支持
**Given** 开发者需要支持新的代理协议
**When** 他们查看解析器模块说明
**Then** 他们应该知道：
- 在 internal/parser/ 目录下创建新的解析器文件
- 各协议解析器是独立实现的
- 需要遵循现有的解析器接口

---

### Requirement: Frontend Architecture Documentation
README.md MUST document the frontend code organization, including pages, components, API client, and state management.

#### Scenario: 开发者了解前端模块
**Given** 前端开发者需要添加新功能或修复 bug
**When** 他们查看前端架构文档
**Then** 他们应该能够：
- 定位到页面组件（web/src/pages/）
  - Dashboard.tsx: 仪表盘页面
  - Subscriptions.tsx: 订阅管理页面
  - Rules.tsx: 规则配置页面
  - Settings.tsx: 系统设置页面
  - Logs.tsx: 日志查看页面
- 找到共享组件（web/src/components/）
- 理解 API 客户端的位置（web/src/api/）
- 了解状态管理方案（web/src/store/ 使用 Zustand）

#### Scenario: 开发者添加新页面
**Given** 需要添加新的功能页面
**When** 开发者查看前端结构文档
**Then** 他们应该知道：
- 在 web/src/pages/ 下创建新的页面组件
- 如何在 web/src/api/ 中定义 API 调用
- 如何在 web/src/store/ 中管理状态

---

### Requirement: Architecture Principles Documentation
README.md MUST explain the architectural principles and design patterns that the project follows.

#### Scenario: 开发者理解设计决策
**Given** 开发者需要进行架构决策或重构
**When** 他们查看架构原则章节
**Then** 他们应该理解：
- **单一职责原则**: 每个模块专注于特定功能
- **清晰分层**: API → Service → Storage 的分层架构
- **协议解耦**: 各协议解析器独立实现，易于扩展
- **模块化设计**: 前后端清晰分离，模块间低耦合

#### Scenario: 代码审查时验证架构一致性
**Given** 进行代码审查
**When** 审查者检查新的代码变更
**Then** 他们可以参考架构原则：
- 确认新代码是否违反单一职责
- 验证是否正确使用了分层架构
- 检查新模块是否保持了适当的解耦

---

### Requirement: Bilingual Documentation Consistency
All code structure documentation MUST be provided in both Chinese and English versions, with consistent content across both languages.

#### Scenario: 中文用户查看文档
**Given** 中文用户访问 README.md
**When** 他们滚动到中文部分
**Then** 他们应该能够：
- 看到完整的中文项目结构说明
- 理解各模块的中文描述
- 获得与英文版本相同的信息

#### Scenario: 英文用户查看文档
**Given** 英文用户访问 README.md
**When** 他们阅读英文部分
**Then** 他们应该能够：
- 看到完整的英文项目结构说明
- 理解各模块的英文描述
- 获得与中文版本相同的信息

---

## Cross-References
- 该 spec 不依赖其他 capabilities
- 这是一个独立的文档改进需求
