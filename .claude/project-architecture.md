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
├── .github/
│   └── workflows/
│       └── deploy.yml
├── .gitignore
├── AGENTS.md
├── cmd/
│   └── dev-time-server/
│       └── main.go
├── deploy/
│   ├── dev-time-server.service
│   └── dev-time.nginx.conf
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
│   │   │   ├── 0003_agent_conversations.sql
│   │   │   ├── 0008_repository_analysis_state.sql
│   │   │   ├── 0009_repository_sync_state.sql
│   │   │   └── 0010_github_event_normalized_metadata.sql
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

### `.github/workflows/deploy.yml`

后端生产 CI/CD workflow。Push 到 `main` 或手动触发时，先运行 `go test -p=1 ./...` 和 Linux binary build；验证通过后使用 GitHub Actions Secrets 中的 SSH 凭证上传 binary、systemd unit 和 Nginx vhost 配置到服务器，并重启 `dev-time-server`。

### `deploy/`

生产部署配置目录。`dev-time-server.service` 定义 systemd 运行方式，`dev-time.nginx.conf` 定义 `dev-time.yi-flow.com` 的 HTTP 入口、前端静态资源和 `/api/` 反向代理；`/internal/` 默认不对公网暴露。

### `.claude/product-prd.md`

Dev Time 产品级 PRD。定义产品定位、MVP 范围、风险模型、Agent 场景和视觉方向。

### `.claude/technical-architecture.md`

Dev Time 跨端技术架构。定义 GitHub 事实源、事件流、风险引擎、Agent Runtime、LLM Gateway、通知层和 API 边界。

### `.claude/tech-stack.md`

后端技术栈、工具链、脚本、依赖、安全和验证规范。当前定稿为 Go + PostgreSQL。

### `.claude/coding-standards.md`

后端编码规范、Go / PostgreSQL 数据访问约束、行数约束和评审检查项。

### `cmd/dev-time-server/`

后端服务入口。当前启动 HTTP server，并挂载 `GET /healthz`。默认必须连接 PostgreSQL 并完成 migration；本地前端联调可临时设置 `DEV_TIME_ALLOW_NO_DATABASE=true`，数据库不可用时仍启动 HTTP，`/api/settings/github` 返回未连接状态，避免前端出现网络层 `Failed to fetch`。

### `internal/`

后端内部包目录。

- `api/`：HTTP router 和 handler。当前包含 `GET /healthz`、`GET /api/projects`、`GET /api/github/installations/start`、`GET /api/github/installations/callback`、`POST /api/github/repositories/import`、`POST /api/github/webhook`、`GET /api/projects/{projectID}/risk`、`GET /api/projects/{projectID}/action-suggestions`、`GET /api/settings/github`、`POST /api/settings/github/repositories/discover`、`POST /api/settings/github/repositories/{repositoryID}/load-project`、`PATCH /api/settings/github/repositories/{repositoryID}/analysis`、`POST /api/settings/github/repositories/{repositoryID}/sync`、`GET /api/settings/llm-providers`、`POST /api/settings/llm-providers`、`POST /api/risk-assessments/{assessmentID}/refresh-agent`、`GET /api/risk-assessments/{assessmentID}/evidence-bundle`、`GET /internal/llm-provider-config`、`POST /internal/agent-jobs/claim`、`POST /internal/agent-jobs/{jobID}/complete`、`GET /internal/risk-assessments/{assessmentID}/evidence-bundle`、`GET /api/projects/{projectID}/agent-conversation`、`POST /api/agent-conversations/{conversationID}/turns` 和 `POST /api/action-suggestions/{suggestionID}/confirm`。GitHub settings 现在区分 repository catalog 和 Dev Time project：浏览器安装 GitHub App 后，callback 使用 installation token 拉取可访问 repositories 并写入 catalog；发现到的授权仓库即使尚未加载为项目也会出现在 settings，只有调用 `load-project` 后才进入 `/api/projects` 和 Agent 工作台。GitHub webhook 会在落库时保存 `github_object_type`、`github_object_id` 和 `normalized_summary`；当前 `check_run` 和 `pull_request` 已生成稳定摘要，用于前端证据包和 Agent 证据追溯。AgentJob completion 可在同一事务保存 AgentArtifact 和关联 ActionSuggestion，并返回 `action_suggestion_ids`。
- `buildinfo/`：服务标识和构建信息。
- `config/`：环境变量配置读取。
- `db/`：PostgreSQL migration runner、基础 store API、Event Store、RiskAssessment 持久化、AgentJob 队列、AgentRun / AgentStep 调查时间线、AgentArtifact 保存、LLM provider key 加密存储、AgentConversation / ActionSuggestion 持久化和容器化集成测试。项目风险评估和 EvidenceBundle 读取会检查仓库 `analysis_enabled`，关闭分析的仓库不再返回可分析风险结果。
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

## Agent Conversation 响应契约

`POST /api/agent-conversations/{conversationID}/turns` 会优先调用 `dev-time-agent` session runtime。当前响应和持久化字段包括：

- `agent_response`：最终给用户看的中文回复。
- `evidence_refs`：本轮回答引用的证据。
- `intent`：Agent 识别出的意图。
- `trace_events`：server 持久化的审计事件，由 `reasoning_trace` 转换而来；无 reasoning trace 时保留兼容 synthetic trace。
- `reasoning_trace`：前端可折叠展示的可审计思考过程。
- `tool_calls`：runtime 实际执行的工具调用摘要。
- `approval_request`：写操作草稿的用户确认请求；确认前 server 不执行外部写入。

Agent Tool internal API：

- `GET /internal/risk-assessments/{assessmentID}/project-status`：提供项目风险分、等级、最高风险原因和证据引用。
- `GET /internal/risk-assessments/{assessmentID}/ci-checks`：提供当前风险相关 check_run 摘要。
- `GET /internal/risk-assessments/{assessmentID}/pull-requests`：提供当前风险相关 PR 摘要。
- `GET /internal/github/auth-status`：提供 GitHub 授权/导入仓库可见状态、只读权限清单和可见仓库摘要。
- `GET /internal/github/repositories`：提供 Agent 可读取的 GitHub 仓库与 Project 映射，仅返回用户已加载为项目且开启 `analysis_enabled` 的仓库。
- `POST /internal/action-suggestions`：创建 `pending_user_confirmation` 行动草稿；确认前不执行外部 GitHub 写入。

GitHub internal API 只向 Agent 暴露受控事实，不回传 installation token、OAuth token 或 PAT。GitHub 写操作必须继续走 ActionSuggestion 用户确认链路。

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
| 2026-06-16 | 增加后端 GitHub Actions 生产部署 workflow | 1.0 需要通过 CI/CD 构建 Go binary、配置 systemd/Nginx 并部署到服务器 | `ruby -e 'require "yaml"; YAML.load_file(".github/workflows/deploy.yml")'` |
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
| 2026-06-13 | 透传并保存 Agent reasoning trace | 前端需要按 turn 展示默认折叠的思考过程，同时保留工具调用和审批请求审计 | `go test ./...` |
| 2026-06-13 | 增加 GitHub 只读 internal tools | Agent 需要在用户授权边界内读取 GitHub 可见仓库，不能直接接触 token 或编造访问状态 | `go test ./internal/api -run TestInternalGitHubAuthStatusReportsImportedRepositoryAccess -count=1` |
| 2026-06-13 | 增加 GitHub settings 公开 API | 前端需要展示 GitHub 连接状态、权限和可见仓库 | `go test ./internal/api -run TestGitHubSettingsExposeConnectionStatus -count=1` |
| 2026-06-15 | 增加仓库分析开关和同步状态字段 | 产品研发切到 GitHub 仓库选择闭环，用户需要选择哪些授权仓库进入风险分析，并看到基础同步状态 | `go test ./... -run '^$'` |
| 2026-06-15 | 增加公开 EvidenceBundle 和 GitHub 事件标准化元数据 | 前端风险详情需要展示可解释证据链，Agent 也需要稳定事件摘要而不是直接解释 payload | `go test ./... -run '^$'` |
| 2026-06-15 | Agent internal GitHub tools 过滤关闭分析的仓库 | 用户关闭仓库分析后，Agent 可读取仓库边界必须同步收紧 | `go test ./... -run '^$'` |
| 2026-06-15 | 项目风险读取过滤关闭分析的仓库 | 用户关闭仓库分析后，直接 risk/evidence API 也不能继续返回可分析结果 | `go test ./... -run '^$'` |
| 2026-06-15 | pull_request webhook 生成标准化事件摘要 | PR Doctor 和证据包需要稳定 PR 编号与摘要，不应依赖前端解析 payload | `go test ./... -run '^$'` |
| 2026-06-15 | 增加无数据库本地开发启动模式 | 前端联调 GitHub 设置时，如果 Postgres 未启动，不应表现为 API 网络失败 | `go test ./internal/config ./internal/api -run 'TestLoad|TestHealthz|TestRouterAllowsLocalDevCORS|TestGitHubSettingsWithoutStoreReportsDisconnected' -count=1 && go test ./... -run '^$'` |
| 2026-06-15 | 手动仓库同步完成本地状态闭环 | 当前阶段先让同步按钮完成 succeeded/last_synced_at 状态更新，真实 GitHub backfill worker 后续接入 | `go test ./internal/api -run TestGitHubSettingsCanTriggerRepositorySync -count=1 && go test ./... -run '^$'` |
| 2026-06-15 | 拆分授权仓库目录和 Dev Time 项目加载 | GitHub 连接后需要看到全部授权仓库，并允许任意仓库加载为项目进入 Agent 工作台 | `go test ./internal/api -run 'TestGitHubSettingsListDiscoveredRepositoriesBeforeProjectLoad|TestGitHubSettingsCanLoadDiscoveredRepositoryAsProject|TestProjectsOnlyIncludeLoadedGitHubRepositories' -count=1 && go test ./... -run '^$'` |
| 2026-06-15 | 增加 GitHub App 浏览器安装回调 | 用户需要从前端触发真实 GitHub 授权，而不是只能用 discover 接口模拟仓库授权 | `go test ./internal/api -run 'TestGitHubInstallation(StartRedirectsToGitHub|CallbackImportsInstallationRepositories)' -count=1` |
