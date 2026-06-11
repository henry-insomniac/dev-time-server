# 项目架构

## 项目定位

`dev-time-server`：Dev Time 的后端服务。Dev Time 是面向个人开发者和 2-5 人小团队的 GitHub 项目风险驾驶舱，用于把 GitHub 中的项目活动、交付节奏、阻塞信号、代码健康和计划偏差转成可解释的风险态势。

本文件用于记录当前仓库边界、目录职责、关键架构决策和后续扩展原则。跨前后端的产品定位、MVP、风险模型、Agent 场景和总体技术架构，以 `product-prd.md` 和 `technical-architecture.md` 为准。

## 跨项目基础文档

以下文档属于 Dev Time 产品级基础文档，必须在 `dev-time`、`dev-time-server` 和 `dev-time-agent` 三个仓库中保持同源更新：

- `product-prd.md`：产品定位、目标用户、MVP、风险模型、Agent 场景、信息架构和视觉方向。
- `technical-architecture.md`：GitHub 集成、事件存储、项目模型、风险引擎、Agent Runtime、LLM Gateway、通知层、API 边界和 SVG 架构图。
- `dev-time-agent-architecture.md`：Agent Runtime 服务的定位、边界、工作流、数据契约、安全约束和 MVP 阶段建议。

当变更影响产品定位、风险模型、Agent 落地场景、Agent 服务边界、GitHub 数据边界、跨端技术架构或安全原则时，必须同步更新三个仓库中的对应文档。仅影响后端实现细节、服务模块、数据表、任务队列或本仓库工具链时，只更新本仓库相关文档。

## 当前仓库职责

`dev-time-server` 负责后端能力：

- GitHub App installation、webhook 和增量同步。
- Event Store 和 GitHub payload 标准化。
- Project Model、RiskSignal、RiskAssessment。
- Risk Engine 规则评分。
- Risk Scout、Daily Brief、Milestone Planner、PR Risk、Scope Drift、Action Agent 的运行时。
- LLM Gateway、provider key 加密存储、token 和成本记录。
- Notification Layer。
- GitHub 写入草稿确认后的服务端执行。

后端必须保证风险判断可追溯到 GitHub 证据；不得在日志、API 响应或 AgentRun 中泄露用户 LLM API Key 明文。

## 当前目录结构

```text
.
├── .env.example
├── .gitignore
├── AGENTS.md
├── cmd/
│   └── dev-time-server/
│       └── main.go
├── internal/
│   ├── api/
│   │   ├── action_suggestions_test.go
│   │   ├── agent_jobs_test.go
│   │   ├── agent_conversation_test.go
│   │   ├── evidence_bundle_test.go
│   │   ├── llm_providers_test.go
│   │   ├── projects_test.go
│   │   ├── repositories_test.go
│   │   ├── risk_test.go
│   │   ├── router.go
│   │   ├── router_test.go
│   │   └── webhook_test.go
│   ├── buildinfo/
│   │   ├── buildinfo.go
│   │   └── buildinfo_test.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── db/
│   │   ├── migrations/
│   │   │   ├── 0001_initial.sql
│   │   │   ├── 0002_llm_provider_configs.sql
│   │   │   └── 0003_agent_conversations.sql
│   │   ├── migrations.go
│   │   ├── store.go
│   │   └── store_test.go
│   └── testsupport/
│       └── postgres.go
├── go.mod
├── go.sum
└── .claude/
    ├── README.md
    ├── product-prd.md
    ├── technical-architecture.md
    ├── project-architecture.md
    ├── skill-authoring.md
    ├── bug-fix-log.md
    ├── git-collaboration.md
    ├── tech-stack.md
    └── coding-standards.md
```

## 目录职责

### `AGENTS.md`

Agent 入口文件。用于说明项目目标、协作原则和关键文档索引。任何 Agent 开始工作前都应先阅读该文件。

### `.claude/`

项目长期上下文目录。这里保存架构、规范、协作流程和故障记录，避免重要信息散落在对话或临时笔记中。

### `.claude/product-prd.md`

Dev Time 产品级 PRD。定义产品定位、MVP 范围、风险模型、Agent 场景和视觉方向。

### `.claude/technical-architecture.md`

Dev Time 跨端技术架构。定义 GitHub 事实源、事件流、风险引擎、Agent Runtime、LLM Gateway、通知层和 API 边界。

### `.claude/tech-stack.md`

后端技术栈、工具链、脚本、依赖、安全和验证规范。当前定稿为 Go + PostgreSQL。

### `.claude/coding-standards.md`

后端编码规范、Go / PostgreSQL 数据访问约束、行数约束和评审检查项。

### `cmd/dev-time-server/`

后端服务入口。当前启动 HTTP server，并挂载 `GET /healthz`。

### `internal/`

后端内部包目录。

- `api/`：HTTP router 和 handler。当前包含 `GET /healthz`、`GET /api/projects`、`POST /api/github/repositories/import`、`POST /api/github/webhook`、`GET /api/projects/{projectID}/risk`、`GET /api/projects/{projectID}/action-suggestions`、`GET /api/settings/llm-providers`、`POST /api/settings/llm-providers`、`POST /api/risk-assessments/{assessmentID}/refresh-agent`、`GET /internal/llm-provider-config`、`POST /internal/agent-jobs/claim`、`POST /internal/agent-jobs/{jobID}/complete`、`GET /internal/risk-assessments/{assessmentID}/evidence-bundle`、`GET /api/projects/{projectID}/agent-conversation`、`POST /api/agent-conversations/{conversationID}/turns` 和 `POST /api/action-suggestions/{suggestionID}/confirm`。AgentJob completion 可在同一事务保存 AgentArtifact 和关联 ActionSuggestion，并返回 `action_suggestion_ids`。
- `buildinfo/`：服务标识和构建信息。
- `config/`：环境变量配置读取。
- `db/`：PostgreSQL migration runner、基础 store API、Event Store、RiskAssessment 持久化、AgentJob 队列、AgentRun / AgentStep 调查时间线、AgentArtifact 保存、LLM provider key 加密存储、AgentConversation / ActionSuggestion 持久化和容器化集成测试。
- `testsupport/`：测试辅助能力。当前提供 PostgreSQL Testcontainers 启动、migration 和 Store 初始化。

后续按 `github`、`risk`、`agentjobs`、`actionsuggestions` 等领域扩展。

### `.agents/skills/`

可选的项目级 Agent Skills 目录。只有在项目明确需要可复用 Agent 工作流时才创建。新增 skill 时，应同步说明触发条件、输入输出、验证方式和安全边界。

## 架构原则

- 让目录结构表达职责边界。
- 优先遵循项目已有模式，不为了新功能随意引入新风格。
- 共享逻辑需要有清晰调用边界和验证方式。
- 外部服务、账号、密钥、网络访问和数据写入必须明确安全边界。
- 项目级 skills 应保持触发条件明确，避免把泛用提示词或个人偏好写成长期能力。
- 风险分由规则引擎稳定生成，LLM 负责解释、归因和行动建议，不直接决定不可解释的最终分数。
- GitHub 写入默认生成草稿并等待用户确认，不在 MVP 中自动执行。
- 架构变更必须同步更新本文件。

## 架构变更记录

| 日期 | 变更 | 原因 | 验证 |
| --- | --- | --- | --- |
| 2026-06-10 | 初始化 Agent 项目文档 | 建立项目长期上下文和协作基线 | 已创建 `AGENTS.md` 与 `.claude` 文档 |
| 2026-06-10 | 补充 Dev Time 产品 PRD 和技术架构基线 | 明确 GitHub 风险驾驶舱、风险模型和 Agent 落地场景 | 已新增 `product-prd.md` 与 `technical-architecture.md` |
| 2026-06-11 | 初始化 Go 工程骨架 | 建立 M0 可验证后端基础 | `go test ./...` |
| 2026-06-11 | 增加 health/config 和 PostgreSQL 基线 | 建立 M1/M2 后端可运行入口和数据库 schema 基线 | `go test ./...` |
| 2026-06-11 | 增加 GitHub repository import API | 建立 M3 repo 导入到 Project 的 HTTP 纵向切片 | `go test ./...` |
| 2026-06-11 | 增加 GitHub webhook Event Store | 建立 M4 webhook 事实落库和 delivery 幂等切片 | `go test ./...` |
| 2026-06-11 | 增加 Risk Engine v1 首条规则 | check_run failure 可生成 blocked RiskSignal 和 high RiskAssessment | `go test ./...` |
| 2026-06-11 | 增加项目风险队列 API | `GET /api/projects` 可按风险分降序返回项目 | `go test ./...` |
| 2026-06-11 | 增加 LLM Provider 配置 API | 保存 API key 时加密存储，GET/POST 不回传明文 key | `go test ./...` |
| 2026-06-11 | 增加 Agent internal LLM 配置读取 API | `dev-time-agent` 需要从 server 安全边界读取解密后的 active provider 配置，公开设置 API 仍不回传明文 key | `go test ./...` |
| 2026-06-11 | Agent Conversation 接入 active LLM Provider | 用户在前端 Agent dock 发送问题后，后端用已配置的 OpenAI/DeepSeek OpenAI-compatible endpoint 生成中文回复，并保留 evidence_refs | `go test ./...` |
| 2026-06-11 | 增加 EvidenceBundle internal API | Agent 可通过 risk assessment id 获取受控证据包 | `go test ./...` |
| 2026-06-11 | 增加 Agent Conversation API | 追问可基于 EvidenceBundle 返回 answer 和 evidence_refs | `go test ./...` |
| 2026-06-11 | 增加 ActionSuggestion 确认 API | 待确认草稿可经 confirm endpoint 进入 succeeded 状态并保留 evidence_refs | `go test ./...` |
| 2026-06-11 | 增加 AgentJob queue API | 可创建、claim、complete AgentJob，并保存 AgentArtifact | `go test ./...` |
| 2026-06-11 | 扩展 AgentJob completion payload | Agent 完成任务时可一并保存 ActionSuggestion 草稿并返回建议 ID | `go test ./...` |
| 2026-06-11 | 增加项目 ActionSuggestion 列表 API | 前端可读取并确认当前项目的行动草稿 | `go test ./...` |
| 2026-06-11 | 扩展 EvidenceBundle 相关事件 | Agent 可同时获取失败 check_run 和同仓库 pull_request 事件，用于 PR Doctor 草稿生成 | `go test ./...` |
| 2026-06-11 | 增加 AgentRun / AgentStep 调查时间线 | AgentJob 生命周期可沉淀为可读的 Agent 主导风险调查过程 | `go test ./...` |
