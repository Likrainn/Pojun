# AI 交接指南（接手本项目的 AI 请先完整读完）

> 最后更新：2026-11-07（v0.4.62 已发布并部署测试机）
> 读者：接手破军 Pojun 项目的 AI 助手。本文假设你会读代码、跑测试、操作 Linux。
> 凭据（SSH/数据库密码/LLM key）**不写入本仓库**（它是公开仓库），只存在于维护者本地记忆文件中——需要时向维护者要。

## 1. 项目是什么

**破军 Pojun**（`github.com/chu0119/Pojun`，2026-11-02 前名 ARTEX）是一套 **LLM 多 Agent 自主渗透测试平台**：给一句话目标，自动完成「目标拆解 → 侦察 → 漏洞挖掘 → 利用 → 后渗透 → 报告」全流程，人随时插手纠偏。

- **Go 单体后端**（`github.com/chu0119/pojun`，内嵌 Next.js 静态前端 `-tags embedui`）
- **PostgreSQL** 是唯一持久层（schema.sql 幂等迁移）
- Agent 引擎基于 **norma SDK**（fork：`go.mod` replace 到 `github.com/chu0119/Norma`）
- 定位：Linux 服务器常驻部署，仅用于授权渗透测试/SRC
- 历史：fork 自已删除的 Autumn-27/ARTEX（v0.3.15 基线），此后 700+ 提交为现维护者独立演进；对外叙事一律按**独立新项目**

**完整版本史看 [CHANGELOG.md](../CHANGELOG.md)**（每版都有详录）；本文只讲「现在是什么状态、东西在哪、怎么干活」。

## 2. 仓库与环境地图

| 位置 | 内容 |
|---|---|
| `D:\xiangmu\aiai\ARTEX` | 主仓工作区（Windows，Git Bash） |
| `github.com/chu0119/Pojun` | 主仓（PUBLIC，ghcr.io/chu0119/pojun 镜像随 tag 自动发布） |
| `D:\xiangmu\ARTEX-SKILL` → `chu0119/pojun-skill` | 单技能可移植版（v2.0，与平台方法论同源，改动要双向同步） |
| `github.com/chu0119/Norma` | agent harness SDK fork（go.mod replace 指向它；改 SDK 要单独发版并 bump replace 版本） |
| **Kali 测试机** `192.168.198.128` | 真实运行实例（SSH 用户 xingchuan，paramiko 直连；凭据见本地记忆）。服务：`systemctl {pojun,pojun-hostd}`，目录 `/opt/pojun`，DB 是 PostgreSQL 库 **`artex`**（历史残留名），实例以 `pojun` 用户运行 |
| 本机 Docker | 测试 PG：容器 `pojun-audit-pg`（端口 15432，跑测试前 `docker start`） |
| 公网机 120.55.171.237 | 半部署交接态（用户自管，v0.3.85+nginx），不要主动动 |

**测试数据库**：本机 `POJUN_PG_DSN='postgres://pojun:pojuntest@127.0.0.1:15432/pojun_ci?sslmode=disable'`；跑测试前 `docker exec pojun-audit-pg psql -U pojun -d postgres -c "DROP DATABASE IF EXISTS pojun_ci; CREATE DATABASE pojun_ci;"`（共享库残留数据会造成假失败）。

## 3. 核心机制 → 代码位置（改哪里）

| 机制 | 代码 | 要点 |
|---|---|---|
| 多 Agent 编排闭环 | `server/engine.go`（plannerLoop/workerLoop/runIntent）、`agent/planner.go`、`agent/worker.go`、`agent/mainagent.go` | 图变更防抖唤醒 planner → add_intent 派发 → worker 领取执行写回；**改编排逻辑前先读 engine.go 头部与各分支注释（全是实测踩坑记录）** |
| 双图架构 | `db/exploration*.go`（探索图）、`db/task_scope.go`+`db/assets*.go`（资产图） | 节点表 `exploration_nodes`（kind=goal/intent/fact/finding/hint/digest），payload 是 JSONB——**没有 summary 列，取 `payload->>'summary'`**；`activity` 表**没有 task_id 列，用 `exploration_id`** |
| 意图终态与停因 | `server/engine.go` runIntent 末尾分流、`db/task_context.go` 常量 | 原因码写 `blocked_reason` 列：max_turns/timeout/error/task_timeout/shutdown/killed/**idle_stop**；`ReasonStopHookPrevented` 也有专门分支（v0.4.62） |
| idle-stop（无增量收场） | `agent/idlestop.go` | nudge 3 轮 → prevent 5 轮 → **取消 run context**（`AbortIdleStop` cause）真收场；写回类工具（record_fact/insert_assets/report_finding/save_evidence）既不拦也不计轮；收场原因落 `blocked_reason=idle_stop`（v0.4.62 修复了此前不可见的问题） |
| 任务终态收尾 | `server/engine_timeout.go`（settleTaskWithReason/runFinalPlannerRound/TaskClock）、`server/auto_finish.go` | goal_met/timeout/auto_idle/auto_coverage/auto_rounds；goalless 任务（无 open goal）frontier 抽干直接 done；收尾前 `warnFindingsWithoutReport` 点名空报告漏洞 |
| 终态复活 | `server/goals.go` admitTaskWhen + `server/engine_timeout.go` resetTerminalRevival | 任何终态（done/failed/timeout）复活：清 first_run_at/deadline_at（新预算）+ 清引擎闸门 + 清挂起收尾 + **重开 canceled blocked 意图**；回归测试 `server/task_revival_deadline_test.go` |
| 证据分级与机械验证 | `agent/finding_recorder.go`（validateFindingEvidence/validateMechanicalEvidence）、`agent/tools.go` addFinding（ToolOutputLedger 证据源核验） | critical/high 必须有可机械复现凭据（HTTP 状态码/curl 输出/工具命中），纯版本匹配会被打回；上报时自动回溯绑定同意图产物、自动锚定资产 |
| 技能体系 | `skills/` 目录（21 个）、`agent/assembly.go`+`server/assembly.go`（wireAgentAugment + SkillIndexBlock 注入 renderSystem）、`db/db.go` builtinSkillVisibility | **新 skill 必须做三件事**：①SKILL.md frontmatter（name/description，缺一加载即空）②加进 builtinSkillVisibility 默认绑定（否则守门测试红）③发布包/镜像含它（build.sh 已 `cp -R skills`）。守门测试：`db/skill_visibility_test.go`（每个 skill 必须绑定或有 optOut；src-hunting 知识库完整性） |
| Skill 元工具调用率 | `server/assembly.go`（SkillIndexBlock → renderSystem 注入速查块） | **教训**：Skill 是元工具，模型不被 system prompt 显式提醒几乎不会调（曾 56k 调用仅 8 次）。任何「希望 agent 主动用的东西」都要进 system prompt |
| MCP | `server/server_mgmt.go`（pgSaveMCP 等）、`mcphttp/client.go`（HTTP/SSE）、norma `mcp` 包（stdio）、前端预设 `web/src/app/(main)/system/mcp/page.tsx` mcpPresets | 内置 FOFA MCP 在 `mcp-servers/fofa/`（uv 拉起，相对路径靠服务 cwd=/opt/pojun 或 /app）；**预设 9 个**；新增预设改 mcpPresets 数组 |
| js_audit（SPA 源码泄露） | `agent/tools_jsaudit.go` | 检测链：HTML→script/chunk→sourceMappingURL+`.map` 兜底→sourcesContent 还原落盘→密钥/接口扫描；**SPA fallback 200≠命中**（要 JSON+sources 才算） |
| http_probe / auth_request / diff_responses / api_contract | `agent/tools_probe.go` 等 | 模式统一：**只落脚本+组命令，不自己发请求**——流量照常过 MITM 留证、审批照常生效。新领域工具照这个模式写 |
| 漏洞情报 | `vulnintel/`、`agent/tools_vulnintel.go`（search_vulns/get_vuln/get_poc/scan_with_nuclei） | NVD+KEV+GHSA+PoC 同步；版本区间三态判定；「版本匹配≠漏洞」 |
| 拦截三层闸门 | `intercept/`、`guard/`、`db/intercept_builtin_rules.go` | deny/judge/ask；规则分批种子（settings flag 记批次，用户删改不复活） |
| 任务 Playbook | `db/task_playbook_seed.go` | 4 个内置模板（Web 全流程/内网域/APP+API/纯侦察）；settings flag `task_playbooks_v1` |
| Word 报告 | `report/`（手写 OOXML 零依赖）、`report/fonts/` 公文字体 | 字体内嵌 odttf；docx→PDF 渲染验证配方见记忆「docx 渲染验证配方」 |
| 前端 | `web/src/`（Next.js 静态导出），构建 `NEXT_EXPORT=1 npx next build` → 拷 `web/out` → `server/webui/dist` | 图片缩放 ZoomableImageDialog；数据查看器 ArtifactDataViewerDialog；MCP 预设在 system/mcp/page.tsx |

## 4. 发版流程（标准动作，别跳步）

1. **改代码 + 测试**：`go build ./... && go vet ./...`；测试库刷新（见 §2）；`go test ./... -count=1`（已知并行 flake 见 §6，单跑复验判定）；前端改动跑 `cd web && npx tsc --noEmit && NEXT_EXPORT=1 npx next build`。
2. **同步前端**：`rm -rf server/webui/dist && cp -r web/out/. server/webui/dist/`（**改了前端必须重做**，否则二进制内嵌旧前端）。
3. **CHANGELOG.md** 加条目（锚点用 `assert` 防 replace 静默失败）。
4. **commit + tag + push**：`git add -A && git commit && git tag v0.4.x && git push origin main v0.4.x`。版本号小步递增（用户明确要求不提大版本）。本机网络对 GitHub 不稳：push 失败先重试，再走仓库级代理（已配 `git config http.proxy http://127.0.0.1:7897`）。
5. **CI（release.yml）自动**：交叉编译 linux amd64+arm64 → zip+SHA256SUMS → GitHub Release → **docker 多架构镜像推 ghcr**。⚠️ CI 会用自己的打包覆盖手动上传的同名资产（属正常，最终自洽）；CI 的 SHA256SUMS 若内容陈旧要手动覆盖。
6. **部署 Kali**（paramiko 配方）：sftp 上传 `dist/pojun-linux-amd64/pojun` → `systemctl stop pojun`（hostd 一并）→ `install -m 0755 /tmp/pojun /opt/pojun/.pojun.new && mv -f ... /opt/pojun/pojun`（ETXTBSY 安全换装）→ `chown pojun:pojun` → start → `journalctl -u pojun | grep POJUN` 核对版本行 → `/api/health` 200。若改了 skills/mcp-servers/前端，这些也要同步（或直接跑 install.sh 走 file:// 本地源）。

## 5. 测试纪律

- **并行 flake 名单**（全量偶发挂、单跑必过，非代码问题）：`TestChatMentionWorkerReceivesServerDetails`、`TestConversationBatchDeleteReportsMissing`、`TestNotifyDeliveriesHistoryAndRetry`、`TestNotifyChannelAPICreateValidation`、`TestTaskTemplateHTTPCRUD` 等——判定法：单跑复验。**新代码导致的新失败必须修，不许拿 flake 当挡箭牌**。
- Windows 本机 `go test ./docpreview` 偶发被 Defender 锁 exe——`go test -c -o` 手动编译执行。
- Git Bash 中文 curl 会 GBK 乱码：JSON body 写 UTF-8 文件 `--data-binary @file`。
- paramiko 的 shell 不认 bash 数组；多步脚本用 `bash -s` 写 stdin。
- Kali 上 psql：`activity` 用 `exploration_id`、`exploration_nodes` 用 `payload->>'summary'`；复杂 SQL 写文件 base64 投递 `psql -f`。

## 6. 已知问题与未决事项（接手后可做的方向）

1. **skill 使用率观测**（v0.4.60 速查注入后待验证）：跑一个任务看 activity 里 `Skill` tool_use 是否出现。
2. **compactor 降本**：压缩输出 9.4M token 是最贵单项（复盘 docs/复盘-2026-11-06.md）；候选=调低 compaction_max_tokens + 工具输出早裁剪。
3. **Metasploit RPC 桥**（PentAGI 思路）：HexStrike 预设已给（v0.4.60），观察接入效果再决定是否原生桥。
4. **SecLists 未装 Kali**（2.5G，GitHub 大文件下载被墙）——网络好时装。
5. **Docker Hub 未发布**：需维护者配 DOCKERHUB_USERNAME/TOKEN secrets；README 只宣传 ghcr。
6. **任务 18 paused**（渭南，19 条 open 意图）：用户示意后恢复；恢复时会走终态复活全流程（v0.4.57 修复）。
7. **低产目标预期管理**：开局窄面提示已做（<10 资产）；可进一步做「预期产出 vs 实际」差距提示。
8. **并行测试 flake 加固**：共享 PG 库的测试隔离（当前靠刷新库绕）。

## 7. 用户偏好速记（违者返工，详见记忆文件）

- **小版本小步走**，不提大版本；每批独立发版。
- **目标数据明文保真**（第一方递交官方，不打码）；**平台自身凭据仍掩码**。
- **复测纯手动**，禁止自动复测/提醒。
- **SQL 注入报告必须带实测跑通的 sqlmap 复测命令**。
- **任务该结束就结束**，禁止无限扫描（自动收尾判据已内置，设置页可调）。
- **GitHub 门面**：只发 Linux、README 一条命令安装、Contributors 只留维护者。
- **外部资产融合**：整包迁入 + 分发链路打通 + 分层验证（单测/端到端/部署侧）全绿才打包。
- **线上 bug 走标准流程**：数据实证根因 → 代码修复 → 回归测试 → 发版部署。
- 中文交流；Kali 可随意操作但**升级/重启前查非终态任务**；未经明确指示不替用户重跑真实扫描。

## 8. 文档索引

- [CHANGELOG.md](../CHANGELOG.md) — 全部版本详录（权威）
- [复盘-2026-11-06.md](复盘-2026-11-06.md) — 12 任务效率/成本/通道数据基线
- [同类工具对比-2026-11-06.md](同类工具对比-2026-11-06.md) — 14 项目对比与融合清单
- [01-双图架构.md](01-双图架构.md) ~ [04-审批与拦截.md](04-审批与拦截.md) — 架构设计
- [部署手册.md](部署手册.md) — 部署/升级/排障踩坑全录
- [公网部署安全加固-2026-10-29.md](公网部署安全加固-2026-10-29.md) — 公网基线
- [模型路由策略.md](模型路由策略.md) — 按角色配模型
