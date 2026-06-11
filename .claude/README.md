# .claude 文档索引

`.claude` 目录保存 `dev-time-server` 的长期上下文，供人类维护者和 Agent 在开发、排障、评审时快速理解项目约定。

## 文档列表

- `product-prd.md`：Dev Time 产品需求、MVP 边界、风险模型、Agent 场景和视觉方向。
- `technical-architecture.md`：跨前后端技术架构、数据流、风险引擎、Agent Runtime、LLM Gateway 和 SVG 架构图。
- `dev-time-agent-architecture.md`：`dev-time-agent` Agent Runtime 服务的定位、边界、工作流、数据契约和安全约束。
- `project-architecture.md`：项目定位、目录职责、架构约束和扩展原则。
- `skill-authoring.md`：项目级 skills 的编写、安装和维护规范。
- `bug-fix-log.md`：bug 修复记录、复盘模板和已知问题。
- `git-collaboration.md`：分支命名、提交信息、PR、评审和发布约定。
- `tech-stack.md`：当前技术栈、推荐工具链、脚本和文档规范。
- `coding-standards.md`：后端编码规范、数据访问约束、行数约束和评审检查项。

## 维护规则

- `product-prd.md` 和 `technical-architecture.md` 是产品级基础文档；涉及产品定位、MVP、风险模型、Agent 场景、Agent 服务边界、跨端架构时，必须同步更新 `dev-time`、`dev-time-server` 和 `dev-time-agent` 三个仓库中的对应文档。
- 修改项目结构时，同步更新 `project-architecture.md`。
- 新增、删除或重命名项目级 skill 时，同步更新 `skill-authoring.md`。
- 修复 bug 后，同步更新 `bug-fix-log.md`。
- 调整协作流程时，同步更新 `git-collaboration.md`。
- 引入新语言、运行时、包管理器、测试框架或格式化工具时，同步更新 `tech-stack.md`。
- 调整代码组织、文件职责、函数职责或行数上限时，同步更新 `coding-standards.md`。

## 当前状态

本目录由脚手架在 2026-06-10 初始化，并在同日补充 Dev Time 产品 PRD 和技术架构基线。请根据项目真实实现继续补充架构、技术栈和验证命令。
