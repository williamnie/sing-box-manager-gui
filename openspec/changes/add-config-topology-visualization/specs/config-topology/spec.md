## ADDED Requirements

### Requirement: 配置拓扑分层展示

系统 MUST 基于当前配置生成可视化拓扑，并以“上游接入 -> 内部路由 -> 出口”分层展示关键链路。

#### Scenario: 展示核心流向
- **GIVEN** 用户打开配置拓扑视图且存在有效配置
- **WHEN** 系统完成配置解析
- **THEN** 必须展示 Inbounds、Route/Policy、Outbounds 三层节点
- **AND** 必须展示从入口到出口的方向性连接

#### Scenario: 展示默认回退路径
- **GIVEN** 配置包含默认出口（final outbound）
- **WHEN** 系统渲染拓扑
- **THEN** 必须在图中明确标识默认回退路径

### Requirement: 拓扑交互可读性

系统 MUST 提供基础交互能力，帮助用户定位节点和链路关系。

#### Scenario: 节点详情查看
- **GIVEN** 用户点击任意拓扑节点
- **WHEN** 节点包含可展示元信息
- **THEN** 系统必须显示节点详情（类型、标识、关联字段）

#### Scenario: 链路高亮
- **GIVEN** 用户选中某节点
- **WHEN** 该节点存在上下游关系
- **THEN** 系统必须高亮相关连线与关联节点

### Requirement: 异常拓扑提示

系统 MUST 对配置中的异常引用或不完整链路进行可见提示。

#### Scenario: 缺失引用告警
- **GIVEN** 某规则或策略引用了不存在的 outbound
- **WHEN** 系统生成拓扑
- **THEN** 系统必须在告警区域和图中给出该异常提示

#### Scenario: 空配置回退
- **GIVEN** 当前配置为空或解析失败
- **WHEN** 用户进入拓扑视图
- **THEN** 系统必须显示可读的空状态或错误状态说明

