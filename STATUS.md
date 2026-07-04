# STATUS — CH-002 agent-manager (am) 整合

> 熱副本，邊做邊寫。變更來源：`docs/changes/change-add-am-integration.md`（CH-002）。
>
> **狀態：CH-002 文件 + 程式 + 單元測試全部完成。** build ok、`go test ./...` 全通過、`golangci-lint run` 0 issues、CLI 煙測通過。尚未 commit。

## 目標

讓 gogents 成為 am 的執行面：`local` 任務源（讀 `~/.am/tickets/`）、ticket `verify` 驗證、agent 工作目錄改 workspace 根、runtime 註冊寫入 `~/.gogents/config.yaml`、state.yaml `runtime_state`、觀測 CLI（`gogents runtime list/status`、`gogents logs`）。

## 進度

### ✅ 已完成：docs/ 與根目錄文件（本階段）

- **根目錄**：`README.md`（Pluggable Task Sources、verify、observability、am 整合節、config 兩型別範例）、`AGENTS.md`（目錄導覽、TL;DR 定位點、cmd 子指令）
- **需求層**：`requirement.md`（假設/索引/Stage 5/依賴圖/檔案表/術語）；補充 `R02`（source local + config.yaml 註冊表 + 前置檢查）、`R03`（task_id/source 隨 source.type）、`R04`（啟動註冊步驟 + `runtime_state` schema 與枚舉）、`R06`（agent WorkDir=workspace 根 + Base Prompt repo 子目錄）、`R07`（verify 路徑 A / fallback 路徑 B）；新增 `R11-local-source.md`、`R12-runtime-observability.md`
- **設計層**：`design-plan.md`（模組表 + 撰寫順序 + D07/D08 定位）；補充 `D01`（Config 加 local/path、GlobalConfig/RuntimeRegistration/GlobalConfigStore）、`D02`（dispatcher 五 factory 加 local、state.yaml `runtime_state`、`SetRuntimeState`、`RuntimeState` 枚舉、啟動註冊步驟、主迴圈 runtime_state）、`D03`（B6 WorkDir、B7a verify、B12 runtime_state、TaskVerifier §4.6、Deps 加 Verifier/SetRuntimeState、Run 流程 Step 5.5、finalize runtime_state、Base Prompt、事件）、`D06`（WorkDir 註解）；新增 `D07-local-source.md`、`D08-runtime-cli.md`
- **測試規劃**：`docs/tests/unit-tests.md`（D01-17~20、D02-13~16、D03-12~16、D06-10；新增 D07、D08 區塊）

### ✅ 已完成：程式碼 + 單元測試

- **`internal/foundation/`**：`config.go`（`Source.Path` 欄位、`GlobalConfig` 去 `active_runtime`、source.type/agent.type enum 驗證）；新 `runtime_state.go`（`RuntimeState` 枚舉，置於 foundation 供 daemon+execution 共用免 import cycle）；新 `global_config.go`（`GlobalConfigStore`：Load/Upsert/Resolve，upsert-by-workspace、原子寫入）
- **`internal/source/`**：新 `local.go`（`local` 五個 concrete：TaskSource/SourceProbe/ContentFetcher/TaskVerifier/FeedbackReporter；ticket YAML 讀寫、inline Extra 保留治理欄位、原子回寫）；`task_source.go`（`TaskVerifier` 介面 + `VerifySpec`）；`dispatcher.go`（五 factory 加 `local`、新 `NewTaskVerifierFromConfig` + github `nopVerifier`）
- **`internal/execution/`**：`runner.go`（agent `WorkDir=paths.Root()`、Step 5.5 `verifyVerdict`/`runVerify`、finalize+verify 的 runtime_state、Deps 加 `Verifier`/`SetRuntimeState`）；`input.go`（Base Prompt 加 `<reponame>` 子目錄指示與替換）
- **`internal/daemon/`**：`state_store.go`（`State.RuntimeState` + `SetRuntimeState`）；`daemon.go`（啟動 upsert 註冊、`runtime_state` idle/executing/failed、Deps 加 `StateStore`/`GlobalConfig`）
- **`internal/cli/`**（命令層，見下方後續調整）：`cli.go`（Dispatch + start/runtime/logs handlers）、`query.go`（List/Status/Logs + pid 存活檢核，pid 不存在→`down`）
- **`cmd/gogents/main.go`**：薄進入點，僅 `os.Exit(cli.Dispatch(os.Args[1:]))`
- **測試**：D01-17~20、D02-13~16、D03-12~16、D07-01~12、D08-01~10 全部落地；既有測試（config_test D01-01 補 instance、judge_test renderInputMd 簽章）同步更新

### 驗證結果

- `go build ./...` ✓；`go test ./...` 全通過 ✓；`golangci-lint run` 0 issues ✓（順帶為既有 gitops/github 的 pre-existing G204 補 nosec）
- CLI 煙測：`runtime list`（空/有）、`runtime status`（dead pid→`down`、未註冊→exit 1）、無參數 usage 皆正確

### 關鍵實作決策（補充）

- `RuntimeState` 型別放 **foundation**（非 D02 doc 原寫的 daemon package）以避免 execution→daemon import cycle；已同步修正 D02 §4.5 doc。
- verify 執行放 `runner.go`（`verifyVerdict`）而非 `judge.go`；judge 維持純 result.json 解析作為 fallback（與 D03 §5.1 Step 5.5 一致）。
- runtime_state 落檔統一經 D02 的 `StateStore`：cmd 建立單一 stateStore 實例，同時注入 daemon.Deps 與 execution.Deps 的 `SetRuntimeState` 閉包，避免雙實例競態。

### 未觸碰（非本任務）

- `docs/changes/change-remove-gemini-agent.md`（CH-003：移除 Gemini agent 支援）**✅ 全部完成**
  - **文件層**：README 重整（Architecture 先於 Features）+ 全 Gemini 敘述清除；AGENTS.md、R01/R02/R06、requirement.md、D01/D02/D03/D06/design-plan、unit-tests.md 皆已清理
  - **程式層**：刪 `internal/agent/gemini.go`；dispatcher.go 去 gemini 分支；config.go `agent.type` 校驗收斂為 `{claude}`；dispatcher_test.go / config_test.go 改為驗證「gemini 現為非法值應回錯」
  - **驗證**：`mise run build` ok、`go test ./...` 全通過、`golangci-lint run` 0 issues；`.go` 內 `gemini` 僅剩「非法值回錯」測試案例。尚未 commit
- `docs/changes/change-add-json-output.md`（CH-004：`runtime list/status` 加 `--json` 結構化輸出）**✅ 文件 + 程式 + 測試全部完成**
  - **文件層**：D08 §3.1/§3.4/§4/§6/§7（JSON 契約、DTO+json tag、`last_sync_at` 零值空字串、`{"error":{"code,message}}` 錯誤物件與 `runtime_not_found`/`state_not_found`/`internal` 三 code、驗收案例 G/H）；R12 新增「`--json` 結構化輸出」段；README Observability 補 `--json` 說明；unit-tests.md 加 D08-11~13
  - **程式層**：`query.go` — `RuntimeView` 加 json tag、`StatusView.MarshalJSON`（`LastSyncAt` 零值→`""`、非零→RFC3339）、新 sentinel `ErrStateNotFound` 包進 state.yaml 缺失錯誤；`cli.go` — `runRuntime` 加 `--json` 分支、`extractBoolFlag`（位置容忍：旗標在 target 前後皆可）、`printJSON`（stdout compact JSON）、`emitRuntimeError`+`newRuntimeErrorEnvelope`（error→code 分類、輸出至 stderr、非零 exit）
  - **測試**：`json_test.go` D08-11（list 陣列／空→`[]`）、D08-12（status flat 物件、down、`last_sync_at`/`current_task` 空字串、非零 RFC3339）、D08-13（三 code + envelope marshal）、`extractBoolFlag` 位置容忍
  - **驗證**：`go build`／`go test ./...` 全通過、`golangci-lint run` 0 issues；CLI 煙測 — `list --json`（有/空`[]`）、`status --json`（down、零值`""`、旗標前後）、錯誤（`runtime_not_found`/`state_not_found` 走 stderr、stdout 無 payload、exit 1）、text 模式不變 皆正確。尚未 commit
- `docs/changes/change-filter-queued-tickets.md`（CH-005：local 源只認領 `status == queued` 的 ticket）**文件 + 程式 + 測試全部完成**
  - **文件層**：README（Pluggable Task Sources、am 整合節）、R02（local 認領條件 + config 註解）、R11（ticket 欄位表 `status` 改讀/寫、掃描與認領過濾規則 + 執行中不重領說明、am/gogents 分工）、D07（B3 List、§3.1 `status` 映射、§5.1 List pseudo-code、§7.1 驗收表）、requirement.md 路線圖、unit-tests.md（D07-01a/01b）皆已對齊「先 label 後 `status==queued`」
  - **程式層**：`internal/source/local.go` `List` 在 label 過濾後追加 `t.Status != "queued"` 過濾（am dispatch 閘門）；`local_test.go` 既有 `TestLocalSource_List` fixtures 補 `status: queued`，新增 `TestLocalSource_ListStatusGate`（D07-01a：僅 queued 認領、blocked/done/failed/awaiting_review/缺失/空皆略過）與 `TestLocalSource_ListLabelStatusCross`（D07-01b：label × status 交叉）
  - **驗證**：`go build ./...`、`go test ./...` 全通過、`golangci-lint run ./internal/source/...` 0 issues、`go vet` clean。尚未 commit
- 工作區既有未提交改動：`internal/agent/claude.go`、`internal/daemon/syncer_test.go`、`internal/foundation/workspace.go`（先前 session in-progress）

## 後續架構調整（依你 review 確認）

1. **`worker_state` → `runtime_state`**：gogents 自身概念是 **runtime**（worker 是 am 側詞，am §2.1 worker↔runtime）。全面改名：`foundation.RuntimeState` 型別＋常數（`RuntimeIdle...`）、state.yaml 欄位 `runtime_state`、`SetRuntimeState`、CLI 輸出、所有文件。列舉值不變。am 透過 `gogents runtime status` 查得後於自身層對應 worker（欄位改名對 am 安全）。R04/R12/change doc 補了 term-mapping 註記。
2. **CLI 集中到 `internal/cli`**：`runtimecli` 併入改名 `cli`；`start`/`runtime`/`logs` 全部 handler 移入 `cli.go`，查詢邏輯在 `query.go`；`cmd/gogents/main.go` 縮成薄派發器。命令層集中、start 組裝也在同一套件。D08 §1 補了「實作套件 internal/cli」說明。

> 兩項皆純命名／搬移、無行為變更；build/test/lint 全綠、0 residual、CLI 煙測通過。

## 關鍵決策（本階段）

- **新建獨立文件而非併入既有**：`local` 源 → R11 + D07（比照 D05 concrete strategy 慣例，OCP）；觀測 CLI → R12 + D08。dispatcher 仍在 D02 §4.4。
- **task_id 泛化**：`github_issue` = `github-<n>`；`local` = ticket `id` 原樣。
- **verify 判定**：ticket 宣告 `verify.cmd` → exit code 判成敗（pr_url 仍取自 result.json，缺則 local 以 `artifacts` 回報）；未宣告 fallback 現行 result.json verdict（github 完全不受影響）。經新 `TaskVerifier` 介面（D03 §4.6）取得 spec，D03 於 workspace 根執行。
- **runtime_state 落檔歸屬**：state.yaml 屬 D02；D03 不直接開檔，透過 D02 注入的 `RuntimeStateSetter`（`StateStore.SetRuntimeState` 窄化）更新 `verifying`/`reporting`/`failed`；`down` 不落檔，由 D08 查詢時依 pid 推得。
- **agent WorkDir = workspace 根**：使 `.claude/`、`.mcp.json` 生效；repo 為 `./<reponame>` 子目錄，Base Prompt 指示；daemon 的 git 操作仍以顯式 repoDir，不受影響。
- **註冊 upsert-by-workspace**：以 workspace 路徑為主鍵，移除 `active_runtime`；註冊失敗不中止 start。

## 參照

- 變更單：`docs/changes/change-add-am-integration.md`
- am 需求：`~/workspace/agent-manager/docs/PRD.md`（§4.2 R5、§4.6 R14、§4.7、§4.8 R15；ticket schema、result 型別、runtime_state 枚舉）
