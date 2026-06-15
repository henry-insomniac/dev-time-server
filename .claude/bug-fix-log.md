# Bug 修复记录

本文件用于记录 `dev-time-server` 的重要 bug、回归、排障结论和修复验证。轻微拼写或纯格式调整不需要记录。

## 记录模板

```markdown
## YYYY-MM-DD - 问题标题

### 现象

用户或系统看到的具体问题。

### 影响

说明影响范围、严重程度和是否阻塞主要流程。

### 原因

定位到的根因。避免只写“逻辑错误”。

### 修复

说明改了什么文件、什么逻辑，以及为什么这样修。

### 验证

列出执行过的命令、手动检查或回归测试。

### 后续

可选。记录需要补充的测试、文档或重构。
```

## 修复记录

## 2026-06-15 - 未配置 Agent Runtime 时 GitHub 项目查询被误判为澄清

### 现象

用户在前端 Agent dock 输入“查看我的 github 项目”时，如果 `dev-time-server` 没有配置 `AgentRuntimeBaseURL` 或 runtime 调用不可用，server fallback 会返回“你想让我评估当前风险、解释证据，还是生成下一步行动计划？”，意图为 `clarify`。

### 影响

影响 GitHub 项目查看和授权状态确认流程。即使 server 已经有 GitHub repository 数据，用户仍无法通过 Agent dock 查看已授权项目。

### 原因

`dev-time-server` 自身的 fallback conversation classifier 没有识别 GitHub 仓库访问类问题，只覆盖普通问候、自我介绍、项目状态、风险解释和行动计划。当前端没有走 `dev-time-agent` runtime 时，GitHub 项目请求直接落入 `clarify`。

### 修复

在 `llm_conversation.go` 增加 `github_repository_list` 意图识别，并在 fallback conversation 路径中通过 `ListGitHubRepositoryAccess` 返回当前已发现/授权的全部 GitHub 仓库列表，不再只返回 `analysis_enabled=true` 的风险分析仓库。无仓库时返回明确的 GitHub 授权/同步提示，不再返回风险澄清文案。

### 验证

- `go test ./internal/api -run TestAgentConversationTurnListsGitHubRepositoriesWithoutRuntime -count=1`
- `go test ./internal/api -run TestAgentConversationTurnListsAllGitHubRepositoriesWithoutRuntime -count=1`
- `go test ./...`

## 已知风险

- 本文件由脚手架初始化，后续应根据项目真实问题持续维护。
- 如果项目尚未建立自动化测试、格式化或 lint 流程，应在 `tech-stack.md` 中补充验证策略。
