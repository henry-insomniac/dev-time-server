# dev-time-agent 架构草案

## 定位

`dev-time-agent` 是 Dev Time 的 Agent Runtime 服务，不是聊天服务，也不是独立产品。它负责从 GitHub 事实和风险信号中构建证据包，运行 Agent 工作流，输出可解释结论和行动草稿。

三服务边界：

```text
dev-time          前端 Web App：风险工作台、Agent 建议展示、用户确认交互
dev-time-server   核心业务后端：事实源、权限、GitHub 集成、风险状态、确认后的写入
dev-time-agent    Agent Runtime：上下文构建、工作流编排、LLM 调用、评估和行动草稿
```

核心原则：server 管事实和权限，agent 管推理和建议。

## 为什么需要独立服务

Agent 是 Dev Time 的核心能力，不只是后端里的一个 LLM helper。真正落地后，Agent 需要：

- 后台持续运行，而不是用户点击后才临时总结。
- 构建复杂上下文：GitHub 事件、历史风险、项目节奏、用户偏好、团队行为。
- 调用受控工具：读取 PR、CI、milestone、历史风险和行动草稿。
- 输出结构化结果，并引用证据。
- 管理 prompt 版本、模型路由、token 成本和失败重试。
- 建立 eval、replay 和 regression snapshot，保证 prompt 变化可评估。

这些能力如果全部放入 `dev-time-server`，会让业务 API、GitHub 同步、风险规则、LLM 调用、Agent 编排和评估体系混在一起，边界会快速失控。

## 目标能力

### Agent Runtime

负责消费 AgentJob，运行对应 workflow，并返回 AgentArtifact。

### Context Builder

负责向 `dev-time-server` 获取受控数据，构建 EvidenceBundle。

### LLM Gateway

负责模型调用、模型路由、结构化输出校验、token 和成本记录。LLM key 的所有权和审计仍归 `dev-time-server`。

### Tool Layer

只提供读工具和草稿生成工具，不直接执行 GitHub 写入。

### Eval System

负责 fixture、replay、snapshot 和质量回归，保证 Agent 可迭代。

## 首批 Agent

### Risk Scout

发现项目风险变化。

输出：风险等级、风险变化、风险类别、影响说明、证据引用、建议下一步。

### PR Doctor

分析 PR 风险。

能力：判断 PR 是否过大、CI 是否阻塞、review 是否停滞、是否需要拆 PR、生成 reviewer comment 草稿。

### Milestone Planner

分析 milestone 是否现实。

能力：判断剩余工作是否超过剩余时间、识别延期风险、建议砍 scope 或调整优先级。

### Scope Guard

识别范围膨胀。

能力：检测 milestone 内新增 issue、需求描述变化、临时插入项和范围漂移语言。

### Daily Brief

生成每日简报。

输出：今天最该处理的 1-3 件事、风险升高项目、风险降低项目、需要确认的行动草稿。

### Action Drafter

将分析结论转成行动草稿。

输出：issue 草稿、PR comment 草稿、label 建议、milestone 调整建议、reviewer 请求草稿。

## 核心数据契约

### AgentJob

```json
{
  "job_id": "job_123",
  "tenant_id": "team_123",
  "project_id": "project_123",
  "risk_assessment_id": "risk_123",
  "agent_type": "pr_doctor",
  "trigger": "ci_failed",
  "requested_by": "system",
  "created_at": "2026-06-11T00:00:00Z"
}
```

AgentJob 只携带 ID 和触发上下文，不直接携带大量 GitHub 原始数据。

### EvidenceBundle

```text
EvidenceBundle
├── project summary
├── current risk assessment
├── risk signals
├── related GitHub objects
│   ├── issues
│   ├── pull requests
│   ├── CI runs
│   ├── commits
│   └── milestones
├── activity timeline
├── historical risk trend
├── user/team preferences
└── allowed actions
```

### AgentArtifact

```json
{
  "agent_type": "pr_doctor",
  "risk_summary": "CI 失败正在阻塞 PR review",
  "evidence_refs": ["ci_run_421", "pr_18", "milestone_v01"],
  "impact": "可能延迟 1-2 天",
  "next_action": "先修复 CI，再请求 review",
  "action_suggestions": ["draft_789"],
  "confidence": "medium",
  "model": "configured-model",
  "prompt_version": "pr-doctor@v1",
  "token_usage": {
    "input": 0,
    "output": 0
  }
}
```

## 服务通信

推荐流程：

```text
dev-time-server
-> 创建 AgentJob
-> 放入 queue
-> dev-time-agent 消费 job
-> dev-time-agent 调用 server internal API 获取 EvidenceBundle
-> dev-time-agent 运行 workflow
-> dev-time-agent 返回 AgentArtifact
-> dev-time-server 保存并展示给前端
```

推荐事件：

```text
risk.assessment.created
risk.assessment.changed
project.synced
pr.blocked
ci.failed
milestone.deadline_near
daily.brief.requested
agent.action.confirmed
```

## 安全边界

- `dev-time-agent` 不直接执行 GitHub 写入。
- `dev-time-agent` 只能生成 ActionSuggestion 草稿。
- 用户确认后，由 `dev-time-server` 校验权限并执行 GitHub 写入。
- LLM provider key 由 `dev-time-server` 加密存储和审计。
- Agent 日志不得记录明文 key、private repo 的非必要完整内容或敏感用户数据。
- 所有 Agent 输出必须引用 evidence_refs，便于前端展示证据链。

## 目录建议

```text
dev-time-agent/
├── src/
│   ├── workflows/
│   │   ├── risk-scout/
│   │   ├── pr-doctor/
│   │   ├── milestone-planner/
│   │   ├── scope-guard/
│   │   ├── daily-brief/
│   │   └── action-drafter/
│   ├── context/
│   │   ├── evidence-bundle-builder/
│   │   ├── project-context-builder/
│   │   └── memory-loader/
│   ├── tools/
│   │   ├── github-read-tools/
│   │   ├── risk-read-tools/
│   │   └── action-draft-tools/
│   ├── llm/
│   │   ├── provider-router/
│   │   ├── prompt-registry/
│   │   ├── structured-output-validator/
│   │   └── token-cost-tracker/
│   ├── evals/
│   │   ├── fixtures/
│   │   ├── replay-runs/
│   │   ├── regression-snapshots/
│   │   └── judge-rubrics/
│   └── artifacts/
└── AGENTS.md
```

## MVP 阶段建议

阶段 1 只实现骨架和两类 Agent：

- AgentJob consumer。
- EvidenceBundle schema。
- Risk Scout workflow。
- PR Doctor workflow。
- ActionSuggestion schema。
- AgentRun log。
- LLM provider adapter。

阶段 2 增加评估：

- fixtures。
- replay runner。
- prompt version。
- output snapshot。
- regression report。

阶段 3 增加自动化：

- daily brief scheduler。
- team preference memory。
- risk trend learning。
- notification priority tuning。

## 暂不做

- 不做泛用聊天助手。
- 不让 Agent 自动写 GitHub。
- 不让 LLM 直接决定最终风险分。
- 不在 Agent 服务中维护 canonical 业务状态。
- 不把 dev-time-agent 做成独立面向用户的产品。
