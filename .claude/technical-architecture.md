# Dev Time 技术架构文档

## 架构目标

Dev Time 的技术架构服务于一个核心目标：从 GitHub 事实源中持续抽取项目状态，生成稳定、可解释、可追溯的项目风险判断，并通过 Agent 把风险转成行动建议。

## 总体原则

1. GitHub 是唯一项目事实源。
2. 事件先落库，再分析，保证风险可回放、可重算。
3. 规则引擎负责稳定评分，LLM 负责解释、归因和行动建议。
4. 所有 Agent 输出必须保留输入摘要、模型信息、证据和结果。
5. 外部写入默认需要用户确认。
6. LLM provider key 必须加密存储，后端不回传明文。


## 服务拆分决策

Dev Time 采用三服务架构：

```text
dev-time          Browser Web App
dev-time-server   Core API + GitHub facts + risk state
dev-time-agent    Agent Runtime + workflow orchestration + evals
```

拆分原则：

- `dev-time` 负责用户体验、风险工作台、Agent 建议展示和用户确认。
- `dev-time-server` 负责用户、团队、权限、GitHub App、webhook、Event Store、Project Model、RiskSignal、RiskAssessment 和确认后的 GitHub 写入。
- `dev-time-agent` 负责 AgentJob 消费、EvidenceBundle 构建、Agent workflow 编排、LLM 调用、结构化输出校验、ActionSuggestion 草稿、prompt 版本和 eval/replay。

`dev-time-agent` 不维护 canonical 业务状态，不直接执行 GitHub 写入，不作为面向用户的聊天服务。

## 形态决策

MVP 优先建设 Web 服务，而不是本地桌面 App。

推荐形态：

```text
Browser Web App
+ API Server
+ GitHub App
+ Background Workers
+ Agent Runtime Service
+ LLM Gateway
+ Notification Layer
```

选择 Web 服务的原因：

- GitHub webhook 需要稳定可访问的服务端入口。
- 风险扫描、定时同步、Agent 分析和通知需要在用户不打开客户端时持续运行。
- 小团队场景需要多设备访问和共享同一套项目风险状态。
- LLM provider key、GitHub installation token、webhook secret 等敏感信息应集中在后端安全边界内处理。
- 事件回放、风险重算、后台任务和通知天然依赖服务端持续运行。

PWA 可以作为 Web App 的体验增强，例如桌面安装、应用图标、浏览器通知和离线壳层。但 PWA 不改变核心架构：GitHub 同步、风险计算、Agent 分析和 LLM 调用仍由后端服务完成。

本地桌面 App、菜单栏提醒和系统级通知可以作为后续扩展。它们应作为 Web 服务的客户端外壳，而不是替代后端持续分析能力。

## 系统分层

```text
Client App
-> API Server
-> GitHub Integration
-> Event Store
-> Project Model
-> Risk Engine
-> Agent Job Queue
-> dev-time-agent
-> AgentArtifact / ActionSuggestion
-> Notification Layer
```

## 架构图

> 当前架构以三服务拆分为准。下方 SVG 是早期整体数据流图，用于理解 GitHub -> 风险 -> Agent -> Dashboard 的主路径；`dev-time-agent` 的独立服务边界以 `dev-time-agent-architecture.md` 为准。


```svg
<svg width="1180" height="760" viewBox="0 0 1180 760" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#07111f"/>
      <stop offset="55%" stop-color="#0a1020"/>
      <stop offset="100%" stop-color="#12091f"/>
    </linearGradient>
    <linearGradient id="panel" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#111f33"/>
      <stop offset="100%" stop-color="#15142a"/>
    </linearGradient>
    <linearGradient id="accent" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0%" stop-color="#00E5FF"/>
      <stop offset="50%" stop-color="#8B5CF6"/>
      <stop offset="100%" stop-color="#FF2E88"/>
    </linearGradient>
    <filter id="glow">
      <feGaussianBlur stdDeviation="3.5" result="coloredBlur"/>
      <feMerge>
        <feMergeNode in="coloredBlur"/>
        <feMergeNode in="SourceGraphic"/>
      </feMerge>
    </filter>
    <style>
      .title { fill: #F8FAFC; font: 700 26px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
      .label { fill: #E2E8F0; font: 600 15px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
      .small { fill: #94A3B8; font: 500 12px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
      .box { fill: url(#panel); stroke: #27415f; stroke-width: 1.2; rx: 12; }
      .hot { stroke: #FF2E88; }
      .cyan { stroke: #00E5FF; }
      .purple { stroke: #8B5CF6; }
      .arrow { stroke: url(#accent); stroke-width: 2.2; fill: none; marker-end: url(#arrowhead); opacity: .9; }
      .grid { stroke: #1f3350; stroke-width: .8; opacity: .35; }
    </style>
    <marker id="arrowhead" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
      <polygon points="0 0, 10 3.5, 0 7" fill="#8B5CF6"/>
    </marker>
  </defs>

  <rect width="1180" height="760" fill="url(#bg)"/>
  <g opacity=".35">
    <path class="grid" d="M40 80 H1140 M40 140 H1140 M40 200 H1140 M40 260 H1140 M40 320 H1140 M40 380 H1140 M40 440 H1140 M40 500 H1140 M40 560 H1140 M40 620 H1140 M40 680 H1140"/>
    <path class="grid" d="M100 40 V720 M220 40 V720 M340 40 V720 M460 40 V720 M580 40 V720 M700 40 V720 M820 40 V720 M940 40 V720 M1060 40 V720"/>
  </g>

  <text x="54" y="54" class="title">Dev Time · GitHub Risk Intelligence Architecture</text>
  <text x="54" y="80" class="small">项目来源只来自 GitHub，核心输出是风险等级、原因解释和下一步行动</text>

  <rect x="54" y="125" width="210" height="118" class="box cyan"/>
  <text x="78" y="158" class="label">GitHub App</text>
  <text x="78" y="184" class="small">Repo / Issue / PR</text>
  <text x="78" y="204" class="small">Commit / CI / Review</text>
  <text x="78" y="224" class="small">Milestone / Release</text>

  <rect x="54" y="315" width="210" height="118" class="box cyan"/>
  <text x="78" y="348" class="label">Webhooks + Sync</text>
  <text x="78" y="374" class="small">实时事件接收</text>
  <text x="78" y="394" class="small">定时补偿同步</text>
  <text x="78" y="414" class="small">GitHub API 限流处理</text>

  <rect x="335" y="125" width="220" height="118" class="box purple"/>
  <text x="360" y="158" class="label">Event Store</text>
  <text x="360" y="184" class="small">标准化 GitHub 事件</text>
  <text x="360" y="204" class="small">保留时间线</text>
  <text x="360" y="224" class="small">支持回放和重算</text>

  <rect x="335" y="315" width="220" height="118" class="box purple"/>
  <text x="360" y="348" class="label">Project Model</text>
  <text x="360" y="374" class="small">Repo / Milestone / Work Item</text>
  <text x="360" y="394" class="small">Owner / Deadline / Status</text>
  <text x="360" y="414" class="small">GitHub 原始对象映射</text>

  <rect x="620" y="95" width="230" height="138" class="box hot"/>
  <text x="646" y="128" class="label">Risk Engine</text>
  <text x="646" y="154" class="small">进度风险</text>
  <text x="646" y="174" class="small">活跃度风险</text>
  <text x="646" y="194" class="small">阻塞 / 质量 / 范围风险</text>
  <text x="646" y="214" class="small">规则评分，可解释可调试</text>

  <rect x="620" y="285" width="230" height="158" class="box hot"/>
  <text x="646" y="318" class="label">Agent Runtime</text>
  <text x="646" y="344" class="small">Risk Scout Agent</text>
  <text x="646" y="364" class="small">Daily Brief Agent</text>
  <text x="646" y="384" class="small">PR Risk Agent</text>
  <text x="646" y="404" class="small">Scope Drift Agent</text>
  <text x="646" y="424" class="small">Action Agent</text>

  <rect x="620" y="520" width="230" height="118" class="box purple"/>
  <text x="646" y="553" class="label">LLM Gateway</text>
  <text x="646" y="579" class="small">OpenAI / Anthropic / Gemini</text>
  <text x="646" y="599" class="small">用户自配 Key</text>
  <text x="646" y="619" class="small">加密存储和用量追踪</text>

  <rect x="915" y="125" width="210" height="118" class="box cyan"/>
  <text x="940" y="158" class="label">Risk Dashboard</text>
  <text x="940" y="184" class="small">项目风险总览</text>
  <text x="940" y="204" class="small">风险趋势</text>
  <text x="940" y="224" class="small">下一步行动</text>

  <rect x="915" y="315" width="210" height="118" class="box cyan"/>
  <text x="940" y="348" class="label">Notification Layer</text>
  <text x="940" y="374" class="small">App 内提醒</text>
  <text x="940" y="394" class="small">Email / Slack 可选</text>
  <text x="940" y="414" class="small">风险升级提醒</text>

  <rect x="915" y="520" width="210" height="118" class="box cyan"/>
  <text x="940" y="553" class="label">GitHub Actions</text>
  <text x="940" y="579" class="small">生成 Issue 草稿</text>
  <text x="940" y="599" class="small">Comment 草稿</text>
  <text x="940" y="619" class="small">Label / Milestone 建议</text>

  <path d="M264 184 C300 184 300 184 335 184" class="arrow"/>
  <path d="M159 243 C159 270 159 285 159 315" class="arrow"/>
  <path d="M264 374 C300 374 300 374 335 374" class="arrow"/>
  <path d="M555 184 C590 184 590 164 620 164" class="arrow"/>
  <path d="M555 374 C590 374 590 364 620 364" class="arrow"/>
  <path d="M735 233 C735 250 735 265 735 285" class="arrow"/>
  <path d="M735 443 C735 470 735 492 735 520" class="arrow"/>
  <path d="M850 164 C880 164 885 184 915 184" class="arrow"/>
  <path d="M850 364 C880 364 885 374 915 374" class="arrow"/>
  <path d="M850 579 C880 579 885 579 915 579" class="arrow"/>

  <rect x="54" y="655" width="1071" height="50" fill="#081827" stroke="#263b59" rx="10"/>
  <text x="78" y="686" class="small">关键原则：GitHub 是唯一项目事实源；规则引擎负责稳定评分；LLM Agent 负责理解、归因、建议和行动草稿；用户始终能追溯每个风险来自哪些 GitHub 信号。</text>
</svg>
```

## 关键模块

### GitHub Integration

职责：

- 管理 GitHub App installation。
- 接收 GitHub webhook。
- 拉取 repository、issue、PR、commit、check run、milestone、release。
- 处理 API 限流、重试、分页和增量同步。

设计要求：

- webhook 和定时同步都写入统一事件表。
- 所有 GitHub 对象保留原始 payload，便于后续重算。
- 对 GitHub 写入类动作必须生成草稿并等待用户确认。

### Event Store

职责：

- 存储标准化后的 GitHub 事件。
- 支持按 repository、project、time range 回放。
- 作为 Risk Engine 和 Agent Runtime 的输入源。

建议字段：

```text
id
provider
repository_id
event_type
github_object_type
github_object_id
occurred_at
payload
normalized_summary
created_at
```

### Project Model

职责：

- 把 GitHub repository 映射为 Dev Time 项目。
- 把 issue、PR、milestone 抽象成可分析工作项。
- 维护项目时间线、状态、owner 和风险上下文。

核心对象：

```text
User
Team
GitHubInstallation
Repository
Project
Milestone
WorkItem
PullRequest
CommitActivity
CIRun
RiskSignal
RiskAssessment
AgentRun
ActionSuggestion
LLMProviderConfig
Notification
```

### Risk Engine

职责：

- 从 Event Store 和 Project Model 生成 RiskSignal。
- 基于规则计算风险分。
- 输出 RiskAssessment。

规则要求：

- 规则必须可配置、可测试、可解释。
- 每个风险分必须能追溯到 RiskSignal。
- LLM 不负责最终分数，只负责补充解释和建议。

### Agent Service Boundary

Agent Runtime 由独立的 `dev-time-agent` 服务承载。

职责：

- 消费 `dev-time-server` 创建的 AgentJob。
- 通过 server internal API 获取 EvidenceBundle。
- 执行 Risk Scout、PR Doctor、Milestone Planner、Scope Guard、Daily Brief、Action Drafter 等 workflow。
- 调用 LLM Gateway，校验结构化输出。
- 生成 AgentArtifact 和 ActionSuggestion 草稿。
- 记录 AgentRun、prompt version、token usage、cost estimate 和 evidence_refs。

Agent 输出必须返回给 `dev-time-server` 保存，并由前端展示。`dev-time-agent` 不直接写 GitHub，不维护 canonical 业务状态。

### LLM Gateway

职责：

- 主要由 `dev-time-agent` 使用，统一适配 OpenAI、Anthropic、Gemini 和 OpenAI-compatible endpoint。
- 管理模型选择、超时、重试、成本记录。
- 隔离用户 API Key。

安全要求：

- API Key 加密存储。
- 前端不能读取明文 key。
- 日志不能输出 key、完整 prompt 中的敏感内容或 GitHub private 数据的非必要片段。
- 支持 provider 级别启用和停用。

### Notification Layer

职责：

- 对风险升级、每日简报、同步失败等事件发出提醒。
- MVP 优先支持 App 内提醒。
- Email、Slack、GitHub comment 可作为后续扩展。

## 主要数据流

### 同步和分析流

```text
GitHub webhook / scheduled sync
-> raw payload
-> normalized event
-> Event Store
-> Project Model update
-> Risk Engine
-> RiskSignal
-> RiskAssessment
-> AgentJob queue
-> dev-time-agent builds EvidenceBundle
-> AgentArtifact / ActionSuggestion
-> Dashboard / Notification
```

### 用户查看风险流

```text
User opens dashboard
-> load project risk ranking
-> select high-risk project
-> view risk details
-> inspect GitHub evidence
-> review Agent suggestion
-> jump to GitHub or confirm action draft
```

### Action Agent 流

```text
RiskAssessment
-> dev-time-server creates AgentJob
-> dev-time-agent generates ActionSuggestion draft
-> dev-time-server stores draft
-> user reviews
-> user confirms
-> dev-time-server executes GitHub write API
-> new GitHub event
-> risk recalculation
```

## API 边界建议

```text
GET    /api/projects
GET    /api/projects/:id/risk
GET    /api/projects/:id/timeline
GET    /api/risk-assessments/:id
POST   /api/risk-assessments/:id/refresh-agent
GET    /api/github/installations
POST   /api/github/repositories/import
POST   /api/github/webhook
GET    /api/settings/llm-providers
POST   /api/settings/llm-providers
POST   /api/action-suggestions/:id/confirm
```

## 前后端职责

### dev-time

前端应用，负责风险驾驶舱、项目详情、Agent Insight、设置页和用户确认交互。前端不保存 GitHub token 或 LLM API Key 明文。

前端体验系统需要支持：

- 状态插图：空状态、同步中、风险稳定、高风险、Agent 分析中、行动完成。
- 语义图标：GitHub 对象、风险类型、Agent 动作三套图标语义。
- 动效状态机：同步、风险重算、Agent 分析、证据链展开、ActionSuggestion 确认。
- 证据链可视化：issue、PR、CI、milestone 等 GitHub 对象之间的引用关系。
- 无障碍降级：所有动效必须尊重 `prefers-reduced-motion`，关键状态不能只依赖颜色或动画表达。
- 资产边界：插图和动效服务于项目状态理解，不作为纯装饰背景。

### dev-time-server

后端服务，负责 GitHub 集成、事件存储、项目模型、风险引擎、AgentJob 创建、AgentArtifact 保存、LLM key 管理、通知和安全边界。

## MVP 技术优先级

1. GitHub App 安装和 repo 导入。
2. webhook + 定时同步。
3. 基础 Project Model。
4. RiskSignal 和 RiskAssessment。
5. Risk Dashboard API。
6. LLM Provider 配置。
7. AgentJob queue 和 `dev-time-agent` 服务骨架。
8. EvidenceBundle schema。
9. Risk Scout 和 PR Doctor workflow。
10. 风险证据链。
11. ActionSuggestion 草稿。

## 需要避免的架构偏差

- 不要先做独立任务系统。
- 不要让用户重复维护 GitHub 已有数据。
- 不要让 LLM 直接决定不可解释的风险分。
- 不要在 MVP 自动写入 GitHub。
- 不要把 UI 做成普通 CRUD 后台。
- 不要把 Agent 做成泛用聊天入口。
