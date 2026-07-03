# CH-002 — 支援 agent-manager (am) 整合

## 1. 背景與動機

`agent-manager`（am）為本機 Agent 運作環境的控制面（Tower），以 gogents 作為執行面：am 產出任務單（ticket）交由 gogents 的 worker daemon 執行，並向 gogents 查詢 runtime 狀態與日誌。本變更使 gogents 能：(1) 從本地檔案任務源接收 am 的 ticket，(2) 依 ticket 宣告的方式做結果驗證，(3) 將結果回報回 am，(4) 讓 worker workspace 正確載入專長配置，(5) 提供 runtime 註冊與觀測介面。

## 2. 變更目標

1. 新增 `local` 任務源型別（TaskSource / SourceProbe / ContentFetcher / FeedbackReporter）。
2. judge 支援 ticket 宣告的 `verify` 驗證方式，未宣告時 fallback 現行 coding verdict。
3. agent 執行工作目錄改為 workspace 根，使 workspace 級 `.claude/` 與 `.mcp.json` 生效。
4. `gogents start` 於啟動時將 runtime 註冊寫入 `~/.gogents/config.yaml`，以 workspace 路徑為 key。
5. state.yaml 增顯式 `runtime_state` 欄位。
6. 新增 CLI：`gogents runtime list`、`gogents runtime status <workspace|name>`、`gogents logs <workspace|name>`。

## 3. 具體需求規格

### 3.1 本地任務源 `local`
- `runtime.yaml` 的 `source.type` 支援 `local`，設定：
  ```yaml
  source:
    type: local
    path: ~/.am/tickets          # am 的統一 ticket inbox
    filter:
      label: <this-worker-label>  # 僅認領此 label 的 ticket
  ```
- **TaskSource**：掃描 `path` 下的 ticket 檔，依 `filter.label` 過濾（比照 `github_issue` 依 label 過濾），import 進 task store。多 daemon 共讀同一 inbox、各取各 label、無爭用。
- **ContentFetcher**：由 ticket 檔內容組出 `input.md`（比照現行由 issue 內容組 input）。
- **SourceProbe**：檢查 `path` 可讀。
- ticket 檔為 YAML，欄位由 am 定義（`id / goal / context / verify / worker(label) / depends_on / approvals / …`）；gogents 只需讀取其執行所需欄位。
- Dispatcher（`internal/source/dispatcher.go`）：`NewTaskSourceFromConfig` / `NewSourceProbeFromConfig` / `NewTaskContentFetcherFromConfig` / `NewFeedbackReporterFromConfig` 各增加 `local` 分支。

### 3.2 結果驗證 `verify`
- ticket 可宣告 `verify`：
  ```yaml
  verify:
    cmd: "pytest tests/…"        # exit code 0 = pass，非 0 = fail
    # 可選 expected artifacts 檢查
  ```
- judge（`internal/execution/judge.go`）：若 ticket 宣告 `verify`，於 workspace 內執行該指令，以 exit code 判 pass/fail；**未宣告時 fallback 現行 coding verdict**（讀 `result.json` 的 `status/pr_url`），保持向後相容。

### 3.3 Agent 執行工作目錄
- 現況：runner 給 agent 的 `WorkDir = repoDir`（`<workspace>/<reponame>`），agent cwd 為 repo，導致 workspace 級 `.claude/skills` 與 `.mcp.json` 不保證載入。
- 變更：agent 執行 `WorkDir` 改為 **workspace 根**，repo 以子目錄 `./<reponame>` 提供給 agent。git fetch/checkout 仍以顯式 `repoDir` 操作、不受影響。
- 目的：使該 workspace 的 `.claude/CLAUDE.md`＋`.claude/skills/`＋`.mcp.json`（專長 rules/skills/工具）被 agent 載入。

### 3.4 結果回報 local FeedbackReporter
- 新增 `local` 型別 FeedbackReporter，於 `processing_started / success / failed` 觸發時，將終態與結果寫回 am 的 ticket 檔 `~/.am/tickets/<id>.yaml`：
  - 更新 `status`（done / failed）。
  - 寫入 `result`：`{type: code, pr_url, branch, commit}` 或 `{type: artifacts, items: […]}`。
- 對稱於 `github_issue` reporter 於 issue 留言＋輪 label；am 讀自身 inbox 即得終態結果。

### 3.5 Runtime 註冊（`~/.gogents/config.yaml`）
- 現況：`GlobalConfig` 結構存在但 `start` 未寫入（config.yaml 目前人工撰寫）。
- 變更：`gogents start` 於啟動時（如同寫 logs）將本 runtime 註冊寫入 `~/.gogents/config.yaml`：
  - **以 `workspace` 路徑為 key**（非 runtime name）——workspace 目錄可能被檔案系統刪除、之後在同路徑新增新的 runtime 配置。
  - 若該 workspace 路徑已存在於註冊表 → **覆蓋更新**其 `name` 與 `registered_at`。
  - **移除無用的 `active_runtime` 欄位**。
- `RuntimeRegistration` 維持 `{ name, workspace, registered_at }`；upsert 以 `workspace` 為主鍵。

### 3.6 Runtime 狀態 `runtime_state`
- state.yaml 增顯式欄位 `runtime_state`，列舉：`idle / executing / verifying / reporting / failed / broken`。gogents 內部單位為 runtime（worker 為 am 側對應詞，am §2.1）。
- daemon 於各階段更新此欄位（`down` 由查詢端依 pid 不存在推得，不落檔）。

### 3.7 觀測 CLI
- `gogents runtime list`：讀 `~/.gogents/config.yaml`，列所有已註冊 runtime（workspace / name / registered_at）。
- `gogents runtime status <workspace|name>`：讀對應 state.yaml，回報 `runtime_state`、PID、當前任務、最近同步；**查詢時檢核 PID**，pid 不存在則回報 `down`（即使 state.yaml 殘留舊狀態）。
- `gogents logs <workspace|name>`：輸出該 runtime 的 `~/.gogents/logs/<hash>/` 日誌（供 am 稽核匯出併入）。

## 4. 交付物

- `internal/source/`：`local` 型別的 TaskSource / SourceProbe / ContentFetcher / FeedbackReporter；dispatcher 分支。
- `internal/execution/judge.go`：`verify` 執行與 fallback。
- `internal/execution/runner.go`：agent `WorkDir` 改 workspace 根。
- `internal/foundation/config.go`：`GlobalConfig` 去除 `active_runtime`、upsert-by-workspace 註冊寫入。
- `internal/daemon/`：`start` 寫 runtime 註冊；state.yaml `runtime_state` 維護。
- `cmd/gogents/`：`runtime list` / `runtime status` / `logs` 子指令。
- 對應單元測試與 `docs/designs/` 設計文件更新。

## 5. 相容性

- `github_issue` 任務源與現行 coding 流程不受影響：`verify` 未宣告時 fallback 現行 verdict；`source.type` 預設維持既有行為。
- `~/.gogents/config.yaml` 由人工撰寫改為 `start` 自動維護；移除 `active_runtime` 需同步既有讀取端（目前為 read-but-not-merged，影響面小）。

## 6. 關聯

- am 需求與流程：`~/workspace/agent-manager/docs/PRD.md`、`design.md`。
