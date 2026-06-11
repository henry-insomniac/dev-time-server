# 技术栈与技术规范

## 当前决策

`dev-time-server` 是 Dev Time 的核心业务后端，负责 GitHub 事实源、权限、同步、事件存储、风险状态、AgentJob 创建和确认后的 GitHub 写入。

服务端技术栈定稿如下：

| 类别 | 选型 | 说明 |
| --- | --- | --- |
| 语言 | Go 1.25 | 适合 webhook、后台任务、权限校验、规则引擎和高并发 API |
| HTTP Router | chi | 轻量、idiomatic，适合可维护 REST API |
| 数据库 | PostgreSQL | 存储 canonical state、GitHub event、风险结果和 Agent 产物 |
| 数据库驱动 | pgx | Go 原生 PostgreSQL driver，支持 PostgreSQL 特性 |
| SQL 生成 | sqlc | SQL-first，生成类型安全 Go 代码 |
| Migration | embed SQL + migration runner | M2 已提供最小迁移执行器；后续复杂迁移再评估 goose / tern |
| 测试 | go test + Testcontainers | 单元测试、PostgreSQL 集成测试和规则引擎回归测试 |
| Job Queue | PostgreSQL 表驱动优先 | MVP 避免过早引入 Redis / Temporal；后续按负载替换 |
| 配置 | 环境变量 + `.env.example` | 不提交真实密钥 |

## 技术规范来源

技术栈规范优先以官方文档为准：

- Effective Go：https://go.dev/doc/effective_go
- Go Code Review Comments：https://go.dev/wiki/CodeReviewComments
- chi：https://go-chi.io/
- pgx：https://pkg.go.dev/github.com/jackc/pgx/v5
- Testcontainers PostgreSQL：https://golang.testcontainers.org/modules/postgres/
- sqlc：https://docs.sqlc.dev/
- PostgreSQL Documentation：https://www.postgresql.org/docs/current/
- PostgreSQL JSON Types：https://www.postgresql.org/docs/current/datatype-json.html

## 后端架构规范

- GitHub 是唯一项目事实源；不得引入独立任务系统替代 GitHub issue / PR / milestone。
- webhook 和 scheduled sync 必须先写入 Event Store，再更新 Project Model 和 RiskAssessment。
- 风险评分由规则引擎生成，LLM 只负责解释、归因和行动建议。
- `dev-time-server` 管事实、权限和 canonical state；`dev-time-agent` 管推理和建议。
- GitHub 写入必须来自用户确认后的 ActionSuggestion，并在服务端重新校验权限。
- LLM Provider 当前只允许 OpenAI 和 DeepSeek；API 必须拒绝后端白名单外的 provider。
- LLM provider key 由后端加密存储，API 不回传明文。
- 后端日志不得输出 GitHub token、LLM key、webhook secret 或 private repo 的非必要全文。

## 目录建议

当前结构按以下方向扩展：

```text
cmd/
└── dev-time-server/
internal/
├── api/
├── config/
├── db/
├── github/
├── projectmodel/
├── risk/
├── agentjobs/
├── actionsuggestions/
├── llmproviders/
├── notifications/
└── worker/
migrations/
queries/
```

## Go 代码规范

- 所有 Go 代码必须通过 `gofmt`，导入排序使用 `goimports`。
- 包名短、小写、无下划线；目录名表达边界，不用泛化的 `utils` 承载业务逻辑。
- handler 只负责解析请求、鉴权入口、调用 service、映射响应；业务逻辑下沉到内部包。
- SQL 写在 `queries/`，通过 sqlc 生成类型安全访问层；不要在 handler 中拼 SQL。
- 数据库事务由 service 层显式控制，事务边界必须能从函数名或调用点看出。
- error 必须携带上下文；对外响应使用稳定错误码，不把内部错误原样返回给前端。
- context 从 HTTP request 或 worker job 入口传入，不在深层函数中新建后台 context。
- 时间字段统一使用 UTC 存储，API 输出 ISO 8601。

## PostgreSQL 规范

- GitHub raw payload 和 normalized payload 可使用 `jsonb`；可查询字段必须提升为结构化列。
- 所有外键、唯一约束、状态枚举约束必须由数据库层兜底。
- migration 必须可重复在空库上执行；禁止手工改库后不补 migration。
- 大表按访问路径建立索引，索引变更需要说明查询场景。
- Event Store 保留原始事件，RiskAssessment 必须能追溯到 RiskSignal。
- AgentArtifact、ActionSuggestion 必须保留 `evidence_refs`、模型信息、prompt version 和 token usage。

## 行数规范

详见 `coding-standards.md`。核心约束：

- Go 普通源文件目标不超过 300 行，超过 400 行必须拆分或在 PR 中说明原因。
- 单个函数目标不超过 60 行；复杂 handler 目标不超过 80 行。
- 单个 package 文件数量持续增长时，优先按领域拆分，而不是创建泛用 helper。
- migration、生成代码和测试 fixture 不受普通行数上限约束，但必须放在明确目录。

## 脚本规范

当前已提供：

```bash
go test ./...
```

`go test ./...` 当前包含 PostgreSQL Testcontainers 集成测试，运行前需要 Docker daemon 可用。

后续引入 sqlc 后补充：

```bash
go test -race ./...
go vet ./...
sqlc generate
```

如引入 migration 工具，必须提供从仓库根目录运行的迁移命令。

## 依赖规范

新增依赖前需要说明：

- 依赖解决什么问题。
- 是否已有标准库、项目内工具或 PostgreSQL 能力可替代。
- 是否会增加部署、观测、运行或维护成本。
- 是否需要网络、账号或密钥。

MVP 阶段避免过早引入 Redis、Temporal、Kafka、复杂 ORM 或通用工作流引擎；先用 PostgreSQL 表驱动 job queue 验证闭环。

## 安全规范

- 不提交 `.env`、密钥、令牌、Cookie、账号凭据。
- 示例配置使用 `.env.example`。
- webhook secret、GitHub private key、LLM provider key 必须只在后端安全边界内读取。
- 所有 GitHub 写入前必须校验 user、repository、installation 和 allowed action。
- 后台任务重试必须幂等，避免重复创建 issue、comment 或 label。

## 验证规范

当前 M0 最小验证命令为：

```bash
go test ./...
```

涉及数据库访问、migration、风险规则和 GitHub 写入时，必须补充集成测试或回归 fixture。
