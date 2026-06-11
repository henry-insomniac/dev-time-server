# Dev Time 产品需求文档

## 产品定位

Dev Time 是面向个人开发者和 2-5 人小团队的 GitHub 项目风险驾驶舱。产品不做泛用项目管理，也不做独立任务系统，而是把 GitHub 中的项目活动、交付节奏、阻塞信号、代码健康和计划偏差转成可解释的风险态势。

一句话定位：

```text
连接 GitHub 的 AI 项目风险分析系统，帮助小团队按时间推进项目，提前发现风险，并给出可执行的下一步。
```

## 目标用户

- 独立开发者：同时维护多个 side project、开源项目或商业项目，需要知道哪些项目正在失控。
- 小团队负责人：团队规模不超过 5 人，需要轻量掌握项目风险，而不希望维护复杂流程。
- AI 辅助开发团队：希望让 Agent 从 GitHub 事实源中持续发现风险、解释风险并建议行动。

## 核心问题

小团队通常不会持续维护完整项目管理系统，真实进展散落在 GitHub 的 issue、PR、commit、CI、milestone 和 release 中。用户真正需要的不是再填一套数据，而是打开软件后立刻知道：

- 哪些项目风险最高。
- 风险来自哪里。
- 是否会影响交付时间。
- 今天最应该处理什么。
- 对应 GitHub 证据是什么。

## 产品原则

1. GitHub 是唯一项目事实源。
2. 风险优先于进度百分比。
3. 每个 AI 判断都必须可追溯到 GitHub 证据。
4. Agent 必须给行动建议，不只给摘要。
5. 默认辅助，不默认代执行；写入 GitHub 前必须由用户确认。
6. 少配置、自动化、轻流程，避免变成大型项目管理系统。

## 范围约束

项目来源只允许来自 GitHub。MVP 不支持手动创建项目、不支持导入 Jira/Linear/Notion、不支持独立任务系统。

MVP 产品形态为 Web 服务：用户通过浏览器访问 Dev Time，后端持续接收 GitHub webhook、执行定时同步、运行风险分析和 Agent 分析。本地桌面 App 不进入 MVP；PWA 可作为后续体验增强，但不能替代后端持续运行能力。


## 服务形态

Dev Time 采用三服务架构：

```text
dev-time          前端 Web App：风险工作台、Agent 建议展示、用户确认交互
dev-time-server   核心业务后端：事实源、权限、GitHub 集成、风险状态、确认后的写入
dev-time-agent    Agent Runtime：上下文构建、工作流编排、LLM 调用、评估和行动草稿
```

`dev-time-agent` 不是聊天服务，也不是独立产品。它是 GitHub 项目风险分析的 Agent Runtime，负责把证据包转成可解释结论和行动草稿。

核心边界：`dev-time-server` 管事实和权限，`dev-time-agent` 管推理和建议。

## MVP 用户路径

```text
安装 GitHub App
-> 选择仓库
-> 同步 issue / PR / milestone / commit / CI
-> 生成项目风险分
-> dev-time-agent 构建证据包并运行 Agent 工作流
-> Agent 解释风险并给出今日行动建议
-> 用户查看证据并跳转 GitHub
```

## MVP 功能

### GitHub 连接

- GitHub App 安装和授权。
- 读取用户可访问的 repository。
- 选择需要纳入 Dev Time 分析的 repository。
- 展示同步状态、最近同步时间和同步失败原因。

### 项目风险驾驶舱

- 展示所有项目的风险排序。
- 高风险项目优先展示。
- 每个项目展示风险等级、趋势、主要原因、预计影响和建议动作。
- 支持按风险类别筛选：进度、活跃度、阻塞、质量、范围、协作。

### 风险详情

- 展示风险评分拆解。
- 展示触发风险的 GitHub 证据。
- 展示风险趋势和关键时间线。
- 展示 Agent 生成的原因解释和下一步建议。

### Agent Runtime

- `dev-time-agent` 消费 AgentJob，构建 EvidenceBundle，并运行对应 Agent workflow。
- 支持 Risk Scout、PR Doctor、Milestone Planner、Scope Guard、Daily Brief、Action Drafter。
- 输出结构化 AgentArtifact，包含原因解释、影响判断、证据引用和行动草稿。
- 支持用户手动刷新一次 Agent 分析。

### Agent 简报

- 生成每日项目简报。
- 汇总最高风险项目、风险变化、今日建议动作和需要确认的行动草稿。

### LLM Provider 配置

- 当前支持 OpenAI 和 DeepSeek；DeepSeek 通过 OpenAI-compatible endpoint 接入。
- 用户必须能在前端 LLM 设置页配置 API Key、base URL 和 model。
- 后端加密存储 key，不向前端回传明文。
- 记录 Agent 调用模型、token 和成本估算。

## 产品交互需求

### 三栏工作台主流程

桌面端首页必须采用定稿 demo 的左中右三栏工作台：

- 左栏为风险队列，按风险分、风险变化时间、是否存在待确认 ActionSuggestion 综合排序。
- 中栏为当前风险主工作区，展示今日最高风险、三服务链路图、风险详情、EvidenceBundle、AgentArtifact 和 ActionSuggestion。
- 右栏为常驻 Agent dock，展示 AgentJob 状态、pipeline、上下文对话和输入框。
- 首次进入默认选中最高风险项目；若不存在高风险项目，选中最近有风险变化的项目；若没有风险，展示稳定态和连接/同步引导。
- 切换左栏项目时，中栏和右栏上下文必须同步切换到该项目；未发送的输入可清空，历史对话按项目保留为后续能力，MVP 可只保留当前会话。
- 风险筛选只改变左栏列表；若当前项目被筛选条件排除，自动选中筛选结果中的最高风险项目。
- 中栏详情默认展示 EvidenceBundle；当 AgentJob 完成并生成 ActionSuggestion 时，可自动切到行动草稿并给出轻量提示。
- 页面滚动不得影响右侧 Agent dock 的输入框位置；桌面端 Agent 输入框永远置底，消息列表只在 dock 内部滚动。

### Agent 对话

Agent 对话不是通用聊天页，而是当前风险上下文的解释和行动入口。

- Agent 的首条消息必须来自当前项目的 AgentArtifact，说明风险结论、主要证据和可追问方向。
- 用户可以追问风险原因、证据可靠性、如果不处理的影响、最短处理路径、是否需要调整 scope、能否生成草稿。
- Agent 回答必须基于当前 EvidenceBundle、AgentArtifact 和 ActionSuggestion，不得凭空引用不存在的 GitHub 对象。
- 每个关键回答必须能指向 evidence_refs；UI 中应允许用户从回答跳转或高亮对应证据。
- 用户追问不一定触发完整 AgentJob；默认使用已有 EvidenceBundle 进行轻量回答。
- 当用户要求“重新分析”“刷新风险”“重新读取 GitHub”时，才创建新的 AgentJob。
- Agent 主动发言只发生在明确状态变化时：AgentJob 完成、风险升级、同步失败、ActionSuggestion 生成或用户确认后状态改变。
- LLM provider 未配置、模型调用失败、证据不足或 GitHub 同步过期时，Agent 必须明确说明原因和下一步，而不是输出含糊建议。
- Agent 对话可以生成新的 ActionSuggestion 草稿，但必须保留用户确认边界。

### AgentJob 与对话关系

- `dev-time-server` 根据 RiskAssessment、用户手动刷新或定时任务创建 AgentJob。
- AgentJob 完成后生成 AgentArtifact；AgentArtifact 驱动中栏 Agent 输出和右侧对话首条解释。
- 用户在右侧 Agent dock 追问时，前端应携带当前 project_id、risk_assessment_id、agent_artifact_id 和 conversation context。
- 追问回答可以产生新的 conversational artifact；只有当用户要求生成行动时，才产生 ActionSuggestion。
- 如果 EvidenceBundle 已过期，Agent 应提示先同步 GitHub 或重新运行 AgentJob。
- AgentJob 状态至少包括 queued、running、succeeded、failed、stale。

### ActionSuggestion

ActionSuggestion 是风险闭环的最后一步，必须可查看、可确认、可拒绝。

MVP 支持的草稿类型：

- PR comment：解释阻塞、请求 review、说明修复计划。
- Issue：创建后续任务、scope 冻结任务、风险跟进任务。
- Label 建议：blocked、risk、needs-review、scope-drift 等。
- Milestone 调整建议：延期、拆分、移出低优先级事项。
- Reviewer 请求：建议指定 reviewer 或重新请求 review。
- Follow-up reminder：暂缓处理时生成提醒。

每个草稿必须展示：

- 草稿类型。
- 目标 GitHub 对象。
- 草稿正文。
- 生成原因。
- 证据引用。
- 执行权限要求。
- 预计影响。

用户操作：

- 确认：交给 `dev-time-server` 校验权限并执行 GitHub 写入。
- 编辑：MVP 可先不支持富编辑，但正式产品必须预留编辑入口。
- 拒绝：记录拒绝原因，用于后续 Agent 质量反馈。
- 稍后处理：创建待确认状态或提醒，不关闭风险。

确认后必须展示执行状态：pending、executing、succeeded、failed。写入失败时保留草稿、展示失败原因，并允许重试。

### EvidenceBundle 展示

- EvidenceBundle 至少包含 project summary、current risk assessment、risk signals、相关 PR / issue / CI / commit / milestone、activity timeline、historical risk trend、allowed actions。
- UI 中每条证据必须展示来源类型、标题、摘要、时间和 GitHub 跳转入口。
- AgentArtifact 的关键结论必须能映射到 evidence_refs。
- 证据过期、同步失败、权限失效时，风险判断需要标记为 stale 或 degraded。
- 用户点击证据时，中栏高亮证据详情，右侧 Agent 可以基于该证据继续解释。

### 状态和空状态

必须覆盖以下状态：

- 未连接 GitHub：展示 GitHub App 安装入口和连接后会得到的风险视图。
- 已连接但未选择 repo：展示 repo 选择和推荐导入说明。
- repo 同步中：展示同步进度、最近同步时间和当前读取对象。
- GitHub 权限失效：提示重新授权，不隐藏已有历史风险。
- 无风险：展示稳定态、最近同步时间和继续观察说明。
- 有风险但 Agent 尚未运行：允许用户查看规则风险，并提示运行 Agent 获取解释和行动。
- AgentJob queued / running / succeeded / failed / stale：右侧 dock 必须有明确状态。
- LLM key 未配置：Agent 对话和草稿能力不可用，设置入口清晰可见。
- ActionSuggestion 等待确认：左栏和中栏都要有待确认提示。
- GitHub 写入成功 / 失败：成功后触发风险重算，失败后保留草稿并允许重试。

### 设置和权限

- GitHub App 权限范围必须最小化，MVP 以读取 repo、issue、PR、checks、commit、milestone 为主；写入能力只在用户确认 ActionSuggestion 后调用。
- 用户可以选择和取消纳入分析的 repository。
- LLM provider 配置需要支持用途分配：风险解释、Agent 对话、草稿生成、eval。
- API key 必须加密存储、隐藏展示、支持更新和删除。
- 所有 GitHub 写入前必须由 `dev-time-server` 校验用户权限、repository 权限和 allowed actions。

## 非 MVP 范围

- 手动任务管理。
- 甘特图。
- 时间打卡。
- 本地桌面 App。
- 自动写入 GitHub。
- 复杂团队权限。
- 多项目管理工具导入。
- 自定义工作流引擎。

## 风险模型 v1

风险评分由规则引擎生成，LLM 负责解释、归因和建议。不要把核心评分完全交给 LLM。

### 风险类别

| 类别 | 说明 | 典型信号 |
| --- | --- | --- |
| 进度风险 | 项目可能无法按时间推进 | milestone 临近、issue 关闭速度慢、剩余工作量过高 |
| 活跃度风险 | 项目推进停滞 | 多日无 commit、PR 长时间未推进、issue 无更新 |
| 阻塞风险 | 明确存在卡点 | blocked label、CI 连续失败、PR review 卡住 |
| 质量风险 | 代码或交付质量不稳定 | CI 失败率升高、PR 过大、同一模块反复修改 |
| 范围风险 | 工作范围持续膨胀 | 新增 issue 多于关闭 issue、milestone 内不断插入需求 |
| 协作风险 | 小团队协作链路不顺 | reviewer 响应慢、owner 不清晰、关键工作集中在单人 |

### 风险等级

| 分数 | 等级 | 含义 |
| --- | --- | --- |
| 0-30 | 稳定 | 当前没有明显风险 |
| 31-55 | 关注 | 存在可处理风险，需要观察 |
| 56-75 | 高风险 | 已影响项目推进，需要尽快处理 |
| 76-100 | 紧急 | 可能直接影响交付，需要立即处理 |

### 风险输出格式

每条风险必须包含：

- 风险等级。
- 风险类别。
- 评分和趋势。
- 原因解释。
- 影响判断。
- GitHub 证据。
- 下一步建议。

示例：

```text
风险等级：高
类别：进度 + 阻塞
原因：PR #18 已 3 天未 review，milestone 还剩 2 天，且 CI 最近两次失败。
影响：预计发布至少延迟 1-2 天。
建议：先处理 CI 失败，再请求指定 reviewer review PR #18。
证据：PR #18、CI run #421、milestone v0.1。
```

## Agent 场景 v1

### Risk Scout Agent

持续扫描 GitHub 信号，发现风险并生成风险说明。

输入：issue、PR、commit、CI、milestone、review、release。

输出：风险等级、风险原因、影响范围、建议动作、证据列表。

### Daily Brief Agent

生成每日项目简报。

输出：最高风险项目、风险变化、今日建议动作、需要用户确认的事项。

### Milestone Planner Agent

判断 milestone 是否现实，识别交付压力和范围膨胀。

输出：milestone 风险、拆分建议、优先级建议、延期风险。

### PR Risk Agent

分析 PR 对项目进度和质量的影响。

输出：PR 风险等级、CI 摘要、review 阻塞、拆分建议、review checklist。

### Scope Drift Agent

识别需求和范围漂移。

输出：新增范围说明、影响评估、建议延后或拆分的工作。

### Action Agent

把风险转成可执行动作。

输出：GitHub issue/comment/label/milestone 调整草稿。MVP 阶段只生成草稿，不自动写入 GitHub。

## 信息架构

```text
Risk Workspace
├── Left Rail
│   ├── 风险队列
│   ├── 风险类别筛选
│   ├── 项目风险分
│   └── 待确认 ActionSuggestion 标记
├── Main Work Area
│   ├── 今日最高风险
│   ├── GitHub -> server -> agent -> 用户确认链路图
│   ├── 当前项目风险详情
│   ├── EvidenceBundle
│   ├── AgentArtifact
│   └── ActionSuggestion 草稿
├── Agent Dock
│   ├── AgentJob 状态
│   ├── workflow pipeline
│   ├── 当前风险上下文对话
│   └── 置底输入框
└── Global Header
    ├── GitHub 同步
    └── 手动运行 Agent

Settings
├── GitHub App
├── Repository 管理
├── LLM Provider
└── 通知偏好
```

## 视觉方向

产品视觉方向为 Intelligent Risk Studio。

它不是传统项目管理后台，也不是单调的深色风险控制台。界面应该像一个轻量、聪明、会主动解释的 AI 项目工作室：既能让用户立刻感知风险，也能通过插图、图标和动效降低压力，让个人开发者和小团队感觉“系统在帮我看住项目”，而不是“又多了一个管理工具”。

### UI / 交互基线

正式产品的核心交互和 UI 必须以 `dev-time/demo/index.html` 当前定稿 demo 为基线，不得在正式实现中退回普通卡片看板、顶部 tab 管理后台或单独聊天页。

定稿基线：

- 桌面端采用左中右三栏工作台：左栏风险队列，中栏当前风险主工作区，右栏常驻 Agent dock。
- 右侧 Agent dock 是产品核心交互区，不是附属卡片；它承载 AgentJob 状态、pipeline、上下文对话和行动确认。
- Agent 输入框永远置底；消息列表只在 Agent dock 内部滚动，不跟随中间项目内容滚动。
- 中栏负责当前风险理解：今日最高风险、三服务链路图、风险详情、证据包、AgentArtifact 和 ActionSuggestion。
- 左栏负责跨项目扫描：风险队列、风险筛选、项目切换和风险分识别。
- Agent 对话必须基于当前 EvidenceBundle、AgentArtifact 和 ActionSuggestion，不能做成无上下文的通用聊天。
- 用户确认前，Agent 只能生成行动草稿；确认后的权限校验和 GitHub 写入仍由 `dev-time-server` 完成。
- 视觉语言沿用 demo 的暖纸面背景、强黑描边、酸性绿色 Agent dock、品牌蓝主行动、珊瑚风险色、黄色提醒色、自绘三服务流程图和轻量状态动效。

### 设计气质

- 智能但不冰冷：保留数据可信度，同时用柔和插图和轻量动效降低紧张感。
- 有冲击力但不压迫：风险状态要强，但页面不能被红色警报和霓虹边框淹没。
- 专业但不企业化：面向个人开发者和小团队，避免大型 SaaS 管控感。
- 未来感来自交互和信息组织，不只来自深色背景和发光效果。
- 每个视觉元素都服务于风险理解、Agent 解释或下一步行动。

### 视觉语言

建议采用“风险雷达 + GitHub 星图 + AI 助手面板”的组合语言：

- 风险雷达：用于全局项目态势、风险等级、风险趋势。
- GitHub 星图：把 repo、PR、issue、CI、milestone 表现为有关联的节点和轨迹。
- AI 助手面板：用于承载 Agent 解释、建议动作和每日简报。
- 时间推进线：用于表达项目是否按计划推进。
- 证据卡片：用轻量图标和引用关系展示每个风险来自哪些 GitHub 对象。

### 插图系统

插图不使用通用 stock 风格，也不使用纯装饰插画。每张插图都应该和产品状态绑定：

- 空状态：GitHub repo constellation，表达“连接 GitHub 后开始生成项目星图”。
- 同步中：数据流从 GitHub 节点进入风险引擎，表达系统正在读取项目状态。
- 风险稳定：项目轨道平稳，风险雷达低亮度。
- 高风险：轨道偏移、节点聚集、时间线出现断点，但避免恐吓式警报。
- Agent 分析中：AI 面板扫描 GitHub 证据，逐步生成风险解释。
- 任务完成：风险曲线回落，行动建议被确认。

插图形式建议：

- 半 3D 的几何节点、轨道、时间线和数据片段。
- 轻量纹理、细线、局部高亮，而不是大面积渐变背景。
- 可拆成组件的小插图，服务于空状态、详情页、简报和通知。

### 图标系统

图标要成为产品识别的一部分，不只是按钮装饰。

图标分三类：

- GitHub 对象图标：repo、issue、PR、commit、CI、review、milestone、release。
- 风险类型图标：进度、活跃度、阻塞、质量、范围、协作。
- Agent 动作图标：分析、解释、建议、草稿、确认、提醒。

图标风格：

- 线性为主，局部使用实心状态点。
- 统一 1.75px 或 2px stroke。
- 风险图标允许使用更鲜明的状态色。
- 不使用大号圆角图标卡片堆满页面。
- 按钮优先使用熟悉图标，复杂概念配 tooltip。

### 动效原则

动效要让用户感觉系统在“理解项目”，不是单纯炫技。

核心动效：

- 首屏进入：项目风险卡片按风险等级分层出现，Agent 简报最后浮现。
- 风险变化：分数变化时使用短暂的数字滚动、趋势线过渡和状态色迁移。
- Agent 分析：展示“读取证据 -> 归因 -> 建议动作”的三段式进度，而不是普通 loading spinner。
- 证据链展开：点击风险原因时，相关 issue、PR、CI 节点被高亮并连线。
- GitHub 同步：同步时使用节点脉冲和数据流线，完成后收敛为最新时间戳。
- ActionSuggestion 确认：草稿确认后用轻量完成反馈，并触发风险重新计算状态。

动效约束：

- 尊重 `prefers-reduced-motion`。
- 交互反馈 100-150ms，状态切换 200-300ms，首屏编排不超过 800ms。
- 主要使用 transform 和 opacity，不用抖动、弹跳或长时间循环动画。
- 不在高密度数据区域持续播放动效，避免干扰阅读。

### 配色方向

避免单一深蓝紫霓虹。建议使用“冷静底色 + 风险语义色 + 温暖辅助色”：

```text
深色底色：#07111F / #0B1020
浅色底色：#F6F2EA
主文本：#F8FAFC
浅色文本：#172033
次级文本：#94A3B8
边框：#263B59
AI 青色：#00D5FF
智能紫：#8B5CF6
风险洋红：#FF2E88
警告琥珀：#F59E0B
成功绿色：#22C55E
紧急红色：#EF4444
温暖辅助色：#F7C873
```

深色用于风险驾驶舱和分析态，浅色或暖色区域用于设置、空状态、引导和成功反馈。这样既保留 AI 未来感，又避免产品一直处在高压黑色控制台里。

### 页面体验重点

- 首页第一眼仍然是风险，不是插图；插图用于解释状态和降低认知压力。
- Agent Insight 面板要像“项目副驾驶”，持续解释、提醒和建议。
- 空状态要鼓励用户连接 GitHub，并用插图说明连接后会得到什么。
- 高风险状态要明确但克制，避免整页警报化。
- 数据密集区域保持清晰，插图和动效退到辅助层。

## 成功标准

MVP 成功不以功能数量衡量，而以风险闭环衡量：

- 用户能在 5 分钟内连接 GitHub 并导入 repo。
- 用户打开首页能立刻知道最高风险项目。
- 每个风险都有清晰原因和 GitHub 证据。
- Agent 建议能转化为明确下一步。
- 用户不需要手动维护第二套项目数据。

### MVP 验收标准

- 用户打开首页 3 秒内看到最高风险项目、风险分和最主要原因。
- 左栏风险队列默认按风险优先级展示，切换项目后中栏和右侧 Agent dock 同步切换上下文。
- 桌面端保持左中右三栏；右侧 Agent 输入框永远置底，消息列表内部滚动。
- 每个高风险项目至少展示 1 条 GitHub 证据，证据可跳转原始 GitHub 对象。
- 每个 AgentArtifact 必须包含 evidence_refs，前端能展示或高亮对应证据。
- 用户能在右侧 Agent dock 完成一次追问，并得到基于当前 EvidenceBundle 的回答。
- 用户能从 Agent 对话或中栏详情生成至少一种 ActionSuggestion 草稿。
- 确认 ActionSuggestion 前，系统不会写入 GitHub。
- 确认后由 `dev-time-server` 执行权限校验和 GitHub 写入，并展示 succeeded 或 failed 状态。
- LLM key 未配置、AgentJob 失败、GitHub 同步过期时，UI 有明确状态和下一步入口。
- demo 中确定的视觉语言和核心布局在正式实现中保持一致。
