# R06 - 任務執行流程

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 daemon 從 queued 任務轉為 processing 後,到 agent CLI 結束前的完整執行流程,包含 git 準備、input.md 組裝、agent CLI 呼叫、output 收集。

## 觸發時機

由 daemon 主迴圈(R04)在一次 sync 完成後呼叫。每個 sync 週期最多執行一個任務。任務選擇規則:

- 掃描 `<workspace>/.gogents/tasks/*/task.yaml`
- 挑選 status=`queued` 的任務,依 task_id 字典序取第一個
- 略過 status=`deleted` 的任務

## 執行步驟總覽

```text
1. 更新 task.yaml 狀態為 processing,寫入 task.started
2. 以 gh 查詢 issue 完整內容(body + comments)
3. 組裝 input.md
4. 準備 git 分支
5. 呼叫 agent CLI,收集 output.jsonl
6. Agent 結束後交給 R07 處理結果
```

任一步驟失敗,立即將 task 轉為 `failed`、`failure_count` +1,交給 R07 做 GitHub 回報(見 R03、R07)。

## Step 1:狀態切換

- 將 task.yaml 的 status 從 `queued` 改為 `processing`
- 設定 `execution.branch` 為 `task/<task-id>`
- 設定 `execution.started_at` 為當前時間(Asia/Taipei)
- 清除 `execution.finished_at` 與 `execution.pr_url`
- 採用 R03 定義的原子寫入方式
- 寫入 `task.started` 事件(info)

同時更新 state.yaml 的 `current_task`(見 R04)。

## Step 2:查詢 issue 完整內容

透過 `gh` CLI 查詢對應 issue 的完整資訊(至少含 number、title、body、comments、author、labels、url)。具體子指令與 json 欄位參數由實作階段決定。

- 查詢失敗:
  - 若為 not found:視同 R05 的 deleted 處理,task 轉為 `deleted`,不計入 `failure_count`,結束本次執行
  - 其他錯誤:task 轉為 `failed`,`failure_count` +1,寫入 error 日誌

## Step 3:組裝 input.md

input.md 寫入 `<workspace>/.gogents/tasks/<task-id>/input.md`。內容由兩個明確區塊組成:

### 區塊 1:Base Prompt(固定指令,英文)

由 daemon 程式內建,使用者不可覆寫:

```text
You are a coding agent running inside a prepared workspace.
Your current working directory is the workspace root. The git
repository to work in is the `./<reponame>` subdirectory; run all
git and build commands there. Workspace-level configuration
(`.claude/`, `.mcp.json`) applies to you from the working directory.

## Project Context
Read `AGENTS.md` at the repository root (`./<reponame>/AGENTS.md`)
before starting. It describes the project conventions, architecture,
and constraints you must follow.

## Task Workflow
1. Change into `./<reponame>` and implement the task described in the
   "Task Content" section below on the current git branch.
2. Commit with message format: `<type>: #<issue-number> <description>`.
   Allowed types: feat, fix, chore, docs, refactor, test.
3. Push the branch: `git push origin <current-branch>`.
4. Open a PR via `gh pr create`.
5. When finished (success or unrecoverable failure), write a result
   report to `<absolute-task-dir>/result.json` with fields:
   { "status": "success"|"failed", "pr_url": "...", "message": "..." }

If you cannot complete the task, still write result.json with
status=failed and a clear message explaining why.

The following "Task Content" section contains the original issue
as written by the reporter; it may be in any language. Treat its
language as-is, do not translate.

---
```

`<reponame>` 由 daemon 依 `project.repo.url` 推得後替入(比照 `<absolute-task-dir>`);其餘為固定指令,使用者不可覆寫。

### 區塊 2:Task Content(原始任務內容,語言原樣)

此區塊由 daemon 動態組裝,直接帶入 issue 原始內容(可能是中文、英文、任何語言):

```text
# Task Content

Source: <issue-url>
Issue Title: <issue-title>
Reporter: @<issue-author>

## Description
<issue-body 原樣帶入>

## Comments
### @<comment-author> at <comment-time>
<comment-body 原樣帶入>

...(依序列出所有 comment)
```

### 組裝原則

- 區塊 1 與區塊 2 之間以 `---` 分隔,讓 agent 能明確識別固定指令與原始內容邊界
- `<absolute-task-dir>` 以絕對路徑寫入,避免 agent 對工作目錄的理解差異導致寫錯位置
- issue body 與 comments 的原始文字不翻譯、不改寫,直接帶入

## Step 4:準備 git 分支

在 `<workspace>/<reponame>/` 工作目錄下依序執行:

```text
git fetch origin
git checkout -B task/<task-id> origin/<default-branch>
```

- `-B` 為強制重置,若分支已存在(例如重啟後重跑)會以 default_branch 為基底重建
- 前次若已 push 的 commit 會在後續 agent 的 `git push --force` 下被覆蓋(見 R04 重啟恢復說明)

Git 操作失敗:task 轉為 `failed`,`failure_count` +1,寫入 error 日誌,跳到 R07。

## Step 5:呼叫 agent CLI

由 agent executor 依 `agent.type` 決定實際呼叫方式。目前版本實作 `gemini` 與 `claude`,executor 內部負責:

- 工作目錄設為 **workspace 根** `<workspace>/`,repo 以子目錄 `./<reponame>` 提供給 agent
- 將 input.md 的內容透過 stdin 傳給 agent CLI
- 以對應的模型參數呼叫 agent CLI(參數來自 runtime.yaml 的 `agent.model`)
- agent 的 stdout 重導到 `<task-dir>/output.jsonl`(truncate 後 append)
- stderr 合併到 stdout 或另外處理(由實作決定)
- 環境變數繼承 daemon 行程,不額外注入

具體指令格式由實作階段依 agent CLI 當下的版本決定,本需求不寫死指令字串。

- 寫入 `task.agent_invoked` 事件(info,fields 含 type、model、pid、工作目錄)
- Daemon 同步等待 agent 結束,不設 timeout(目前版本簡化,未來可加)
- Agent 結束後:
  - 寫入 `task.agent_exited` 事件,fields 含 exit code、持續時間
  - 交給 R07 處理結果

## 工作目錄與 workspace 專長配置

agent 子行程的工作目錄為 **workspace 根** `<workspace>/`,而非 repo 目錄。目的是讓 workspace 級的專長配置被 agent 載入:

- `<workspace>/.claude/CLAUDE.md`、`<workspace>/.claude/skills/`:該 worker 專長的 rules 與 skills
- `<workspace>/.mcp.json`:該 worker 的 MCP 工具白名單

repo 以子目錄 `./<reponame>` 提供給 agent;Base Prompt(區塊 1)指明 repo 相對路徑,agent 於該子目錄進行 git commit / push / 開 PR。

daemon 自身的 git 準備(Step 4 的 `git fetch` / `checkout -B`)仍以顯式 repo 路徑 `<workspace>/<reponame>/` 執行,不受 agent 工作目錄變更影響。

## 檔案職責重申

| 檔案         | 寫入者     | 本需求的角色                             |
| ------------ | ---------- | ---------------------------------------- |
| task.yaml    | Daemon     | Step 1 狀態切換                          |
| input.md     | Daemon     | Step 3 組裝                              |
| output.jsonl | Agent CLI  | Step 5 重導目的地                        |
| result.json  | Agent 自身 | 由 agent 在 prompt 指示下寫入,本需求不碰 |

## 不做的部分

目前版本刻意不實作:

- Timeout 機制(不強制中止失控的 agent)
- 資源限制(cgroup、ulimit 等)
- Agent 輸出即時串流到其他管道(只寫檔)
- input.md 模板使用者自訂

## 相關需求

- R02 - Runtime 設定檔(agent.type / model、project.repo.default_branch)
- R03 - 任務狀態模型(queued → processing 轉換、failure_count)
- R04 - Daemon 生命週期(觸發時機、state.yaml.current_task)
- R05 - GitHub Issue 同步(deleted 處理共用)
- R07 - 結果處理(本需求結束後的下一棒)
- R09 - 日誌規範(task.* 事件)
- R10 - 錯誤處理(git / agent 失敗的統一處理)
- R11 - 本地任務源(`local` 型別的 issue 內容以 ticket 檔組裝 input.md)
