# singbox-manager

[English](#english) | [中文](#中文)

---

<a name="english"></a>

## English

A modern web-based management panel for [sing-box](https://github.com/SagerNet/sing-box), providing an intuitive interface to manage subscriptions, rules, filters, and more.

### Features

- **Subscription Management**
  - Support multiple formats: SS, VMess, VLESS, Trojan, Hysteria2, TUIC
  - Clash YAML and Base64 encoded subscriptions
  - Traffic statistics (used/remaining/total)
  - Expiration date tracking
  - Auto-refresh with configurable intervals

- **Node Management**
  - Auto-parse nodes from subscriptions
  - Manual node addition
  - Country grouping with emoji flags
  - Node filtering by keywords and countries

- **Rule Configuration**
  - Custom rules (domain, IP, port, geosite, geoip)
  - 13 preset rule groups (Ads, AI services, streaming, etc.)
  - Rule priority management
  - Rule set validation tool

- **Filter System**
  - Include/exclude by keywords
  - Country-based filtering
  - Proxy modes: URL-test (auto) / Select (manual)

- **DNS Management**
  - Multiple DNS protocols (UDP, DoT, DoH)
  - Custom hosts mapping
  - DNS routing rules

- **Service Control**
  - Start/Stop/Restart sing-box
  - Configuration hot-reload
  - Auto-apply on config changes
  - Process recovery on startup

- **System Monitoring**
  - Real-time CPU and memory usage
  - Application and sing-box logs
  - Service status dashboard

- **macOS Support**
  - launchd service integration
  - Auto-start on boot
  - Background daemon mode

- **Kernel Management**
  - Auto-download sing-box binary
  - Version checking and updates
  - Multi-platform support

### Screenshots

![Dashboard](docs/screenshots/dashbord.png)
![Subscriptions](docs/screenshots/subscriptions.png)
![Rules](docs/screenshots/rules.png)
![Settings](docs/screenshots/settings.png)
![Logs](docs/screenshots/log.png)

### Installation

#### Pre-built Binaries

Download from [Releases](https://github.com/williamnie/singbox-manager/releases) page.

#### Build from Source

```bash
# Clone the repository
git clone https://github.com/williamnie/singbox-manager.git
cd singbox-manager

# Build for all platforms
./build.sh all

# Or build for current platform only
./build.sh current

# Output binaries are in ./build/
```

**Build Options:**
```bash
./build.sh all       # Build for all platforms (Linux/macOS x amd64/arm64)
./build.sh linux     # Build for Linux only
./build.sh darwin    # Build for macOS only
./build.sh current   # Build for current platform
./build.sh frontend  # Build frontend only
./build.sh clean     # Clean build directory
```

### Usage

```bash
# Basic usage
./sbm

# Custom data directory and port
./sbm -data ~/.singbox-manager -port 9090
```

**Command Line Options:**
| Option | Default | Description |
|--------|---------|-------------|
| `-data` | `~/.singbox-manager` | Data directory path |
| `-port` | `9090` | Web server port |

After starting, open `http://localhost:9090` in your browser.

### Configuration

**Data Directory Structure:**
```
~/.singbox-manager/
├── data.json           # Configuration data
├── generated/
│   └── config.json     # Generated sing-box config
├── bin/
│   └── sing-box        # sing-box binary
├── logs/
│   ├── sbm.log         # Application logs
│   └── singbox.log     # sing-box logs
└── singbox.pid         # PID file
```

### Tech Stack

- **Backend:** Go, Gin, gopsutil
- **Frontend:** React 19, TypeScript, NextUI, Tailwind CSS
- **Build:** Single binary with embedded frontend

### Project Structure

```text
singbox-manager/
├── cmd/                    # Application entry point
│   └── sbm/               # Main program entry
│       └── main.go        # Application startup logic
├── internal/              # Core business logic (private packages)
│   ├── api/              # HTTP API routes and handlers
│   │   └── router.go     # Gin router configuration and all API endpoints
│   ├── parser/           # Subscription format parsers
│   │   ├── parser.go     # Parser interface and common logic
│   │   ├── shadowsocks.go    # Shadowsocks protocol parser
│   │   ├── vmess.go          # VMess protocol parser
│   │   ├── vless.go          # VLESS protocol parser
│   │   ├── trojan.go         # Trojan protocol parser
│   │   ├── hysteria2.go      # Hysteria2 protocol parser
│   │   ├── tuic.go           # TUIC protocol parser
│   │   ├── socks.go          # SOCKS protocol parser
│   │   └── clash_yaml.go     # Clash YAML format parser
│   ├── storage/          # Data storage layer
│   │   ├── models.go     # Data model definitions
│   │   └── json_store.go # JSON file storage implementation
│   ├── daemon/           # sing-box process lifecycle management
│   │   ├── process.go    # Process start, stop, restart
│   │   ├── launchd.go    # macOS launchd integration
│   │   └── systemd.go    # Linux systemd integration
│   ├── service/          # Business service layer
│   │   ├── subscription.go   # Subscription management service
│   │   └── scheduler.go      # Scheduled task scheduler
│   ├── kernel/           # sing-box kernel management
│   │   └── (kernel download and version management)
│   ├── builder/          # sing-box configuration generator
│   │   └── singbox.go    # Generate sing-box config.json from user settings
│   └── logger/           # Logging system
│       └── logger.go     # Log management and file rotation
├── pkg/                   # Reusable public packages
│   └── utils/            # Utility functions
├── web/                   # Frontend application
│   ├── src/
│   │   ├── pages/        # Page components
│   │   │   ├── Dashboard.tsx      # Dashboard (system monitoring)
│   │   │   ├── Subscriptions.tsx  # Subscription management
│   │   │   ├── Rules.tsx          # Rule configuration
│   │   │   ├── Settings.tsx       # System settings
│   │   │   └── Logs.tsx           # Log viewer
│   │   ├── components/   # Shared components
│   │   │   ├── Layout.tsx    # Application layout
│   │   │   └── Toast.tsx     # Toast notifications
│   │   ├── api/          # API client
│   │   │   └── index.ts      # HTTP request wrapper
│   │   └── store/        # State management
│   │       └── index.ts      # Zustand store definition
│   ├── embed.go          # Go embed configuration (embed frontend assets)
│   └── package.json      # Frontend dependencies
├── docs/                  # Documentation and screenshots
├── build.sh              # Build script
├── go.mod                # Go module definition
└── README.md             # Project documentation
```

#### Backend Architecture

**Layered Design:**

- **API Layer** (`internal/api`): Handle HTTP requests and routing
- **Service Layer** (`internal/service`): Business logic implementation
- **Storage Layer** (`internal/storage`): Data persistence

**Core Modules:**

- **Parsers** (`internal/parser`): Strategy pattern implementation, each protocol has independent parsing logic, supporting 8 mainstream proxy protocols
- **Process Management** (`internal/daemon`): Cross-platform process management, supporting macOS launchd and Linux systemd
- **Configuration Builder** (`internal/builder`): Convert user configuration to sing-box JSON format

**Architecture Principles:**

- **Single Responsibility**: Each package focuses on a specific functional area
- **Dependency Injection**: Decoupling through interfaces for better testability
- **Protocol Decoupling**: Adding new protocol support only requires adding a new parser file

#### Frontend Architecture

**Tech Stack:**

- **UI Framework**: React 19 + NextUI (modern component library)
- **Routing**: React Router v7
- **State Management**: Zustand (lightweight and clean state management)
- **HTTP Client**: Axios
- **Charts**: Recharts (system monitoring visualization)

**Page Organization:**

- Each functional module corresponds to an independent page component
- Shared Layout provides unified navigation and layout
- Toast component provides global message notifications

### Requirements

- Go 1.21+ (for building)
- Node.js 18+ (for building frontend)
- sing-box (auto-downloaded or manual installation)

### License

MIT License

---

<a name="中文"></a>

## 中文

一个现代化的 [sing-box](https://github.com/SagerNet/sing-box) Web 管理面板，提供直观的界面来管理订阅、规则、过滤器等。

### 功能特性

- **订阅管理**
  - 支持多种格式：SS、VMess、VLESS、Trojan、Hysteria2、TUIC
  - 兼容 Clash YAML 和 Base64 编码订阅
  - 流量统计（已用/剩余/总量）
  - 过期时间追踪
  - 可配置间隔的自动刷新

- **节点管理**
  - 自动从订阅解析节点
  - 手动添加节点
  - 按国家分组（带 emoji 国旗）
  - 按关键字和国家过滤节点

- **规则配置**
  - 自定义规则（域名、IP、端口、geosite、geoip）
  - 13 个预设规则组（广告、AI 服务、流媒体等）
  - 规则优先级管理
  - 规则集验证工具

- **过滤器系统**
  - 按关键字包含/排除
  - 按国家过滤
  - 代理模式：自动测速 / 手动选择

- **DNS 管理**
  - 多种 DNS 协议（UDP、DoT、DoH）
  - 自定义 hosts 映射
  - DNS 路由规则

- **服务控制**
  - 启动/停止/重启 sing-box
  - 配置热重载
  - 配置变更后自动应用
  - 启动时自动恢复进程

- **系统监控**
  - 实时 CPU 和内存使用率
  - 应用和 sing-box 日志
  - 服务状态仪表盘

- **macOS 支持**
  - launchd 服务集成
  - 开机自启
  - 后台守护进程模式

- **内核管理**
  - 自动下载 sing-box 二进制文件
  - 版本检查和更新
  - 多平台支持

### 截图

![仪表盘](docs/screenshots/dashbord.png)
![订阅管理](docs/screenshots/subscriptions.png)
![规则配置](docs/screenshots/rules.png)
![设置](docs/screenshots/settings.png)
![日志](docs/screenshots/log.png)

### 安装

#### 预编译二进制文件

从 [Releases](https://github.com/williamnie/singbox-manager/releases) 页面下载。

#### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/williamnie/singbox-manager.git
cd singbox-manager

# 构建所有平台
./build.sh all

# 或只构建当前平台
./build.sh current

# 输出文件在 ./build/ 目录
```

**构建选项：**
```bash
./build.sh all       # 构建所有平台（Linux/macOS x amd64/arm64）
./build.sh linux     # 仅构建 Linux
./build.sh darwin    # 仅构建 macOS
./build.sh current   # 仅构建当前平台
./build.sh frontend  # 仅构建前端
./build.sh clean     # 清理构建目录
```

### 使用方法

```bash
# 基本用法
./sbm

# 自定义数据目录和端口
./sbm -data ~/.singbox-manager -port 9090
```

**命令行参数：**
| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-data` | `~/.singbox-manager` | 数据目录路径 |
| `-port` | `9090` | Web 服务端口 |

启动后，在浏览器中打开 `http://localhost:9090`。

### 配置

**数据目录结构：**
```
~/.singbox-manager/
├── data.json           # 配置数据
├── generated/
│   └── config.json     # 生成的 sing-box 配置
├── bin/
│   └── sing-box        # sing-box 二进制文件
├── logs/
│   ├── sbm.log         # 应用日志
│   └── singbox.log     # sing-box 日志
└── singbox.pid         # PID 文件
```

### 技术栈

- **后端：** Go、Gin、gopsutil
- **前端：** React 19、TypeScript、NextUI、Tailwind CSS
- **构建：** 单一二进制文件，内嵌前端

### 项目结构

```text
singbox-manager/
├── cmd/                    # 应用程序入口
│   └── sbm/               # 主程序入口点
│       └── main.go        # 应用启动逻辑
├── internal/              # 核心业务逻辑（私有包）
│   ├── api/              # HTTP API 路由和处理器
│   │   └── router.go     # Gin 路由配置和所有 API 端点
│   ├── parser/           # 订阅格式解析器
│   │   ├── parser.go     # 解析器接口和通用逻辑
│   │   ├── shadowsocks.go    # Shadowsocks 协议解析
│   │   ├── vmess.go          # VMess 协议解析
│   │   ├── vless.go          # VLESS 协议解析
│   │   ├── trojan.go         # Trojan 协议解析
│   │   ├── hysteria2.go      # Hysteria2 协议解析
│   │   ├── tuic.go           # TUIC 协议解析
│   │   ├── socks.go          # SOCKS 协议解析
│   │   └── clash_yaml.go     # Clash YAML 格式解析
│   ├── storage/          # 数据存储层
│   │   ├── models.go     # 数据模型定义
│   │   └── json_store.go # JSON 文件存储实现
│   ├── daemon/           # sing-box 进程生命周期管理
│   │   ├── process.go    # 进程启动、停止、重启
│   │   ├── launchd.go    # macOS launchd 集成
│   │   └── systemd.go    # Linux systemd 集成
│   ├── service/          # 业务服务层
│   │   ├── subscription.go   # 订阅管理服务
│   │   └── scheduler.go      # 定时任务调度
│   ├── kernel/           # sing-box 内核管理
│   │   └── (内核下载和版本管理)
│   ├── builder/          # sing-box 配置文件生成器
│   │   └── singbox.go    # 根据用户配置生成 sing-box config.json
│   └── logger/           # 日志系统
│       └── logger.go     # 日志管理和文件轮转
├── pkg/                   # 可复用的公共包
│   └── utils/            # 工具函数
├── web/                   # 前端应用
│   ├── src/
│   │   ├── pages/        # 页面组件
│   │   │   ├── Dashboard.tsx      # 仪表盘（系统监控）
│   │   │   ├── Subscriptions.tsx  # 订阅管理
│   │   │   ├── Rules.tsx          # 规则配置
│   │   │   ├── Settings.tsx       # 系统设置
│   │   │   └── Logs.tsx           # 日志查看
│   │   ├── components/   # 共享组件
│   │   │   ├── Layout.tsx    # 应用布局
│   │   │   └── Toast.tsx     # 提示消息
│   │   ├── api/          # API 客户端
│   │   │   └── index.ts      # HTTP 请求封装
│   │   └── store/        # 状态管理
│   │       └── index.ts      # Zustand store 定义
│   ├── embed.go          # Go embed 配置（嵌入前端资源）
│   └── package.json      # 前端依赖
├── docs/                  # 文档和截图
├── build.sh              # 构建脚本
├── go.mod                # Go 模块定义
└── README.md             # 项目文档
```

#### 后端架构

**分层设计:**

- **API 层** (`internal/api`): 处理 HTTP 请求，路由分发
- **服务层** (`internal/service`): 业务逻辑实现
- **存储层** (`internal/storage`): 数据持久化

**核心模块:**

- **解析器** (`internal/parser`): 采用策略模式，每个协议独立实现解析逻辑，支持 8 种主流代理协议
- **进程管理** (`internal/daemon`): 跨平台进程管理，支持 macOS launchd 和 Linux systemd
- **配置生成器** (`internal/builder`): 将用户配置转换为 sing-box 所需的 JSON 格式

**架构原则:**

- **单一职责**: 每个包专注于特定功能领域
- **依赖注入**: 通过接口解耦，便于测试
- **协议解耦**: 新增协议支持只需添加新的解析器文件

#### 前端架构

**技术选型:**

- **UI 框架**: React 19 + NextUI (现代化组件库)
- **路由**: React Router v7
- **状态管理**: Zustand (轻量级、简洁的状态管理)
- **HTTP 客户端**: Axios
- **图表**: Recharts (系统监控可视化)

**页面组织:**

- 每个功能模块对应一个独立页面组件
- 共享 Layout 提供统一的导航和布局
- 使用 Toast 组件提供全局消息提示

### 环境要求

- Go 1.21+（用于构建）
- Node.js 18+（用于构建前端）
- sing-box（可自动下载或手动安装）

### 许可证

MIT License
