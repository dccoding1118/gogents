# 單元測試案例規劃 (Unit Test Case Planning)

本文檔規劃了各核心模組的單元測試案例，以確保系統在重構與功能擴充過程中的行為一致性。

## D01 Foundation (基礎建設模組)

| 編號   | 測試對象       | 測試情境 (Scenario)                                    | 預期結果 (Expected)                                       |
| :----- | :------------- | :----------------------------------------------------- | :-------------------------------------------------------- |
| D01-01 | ConfigLoader   | Load: 正常讀取 `runtime.yaml`                          | 回傳完整的 Config 結構體                                  |
| D01-02 | ConfigLoader   | Load: `runtime.yaml` 缺失                              | 回傳 `ErrConfigParse` 錯誤                                |
| D01-03 | ConfigLoader   | Load: 缺少必要欄位 (如 `runtime_name`)                 | 回傳 `ErrConfigSchema` 並列出缺失欄位                     |
| D01-04 | ConfigLoader   | Load: 未設定 `max_failures`                            | `max_failures` 自動補齊為預設值 3                         |
| D01-05 | ConfigLoader   | Preflight: 所有工具檢查均通過                          | 回傳 `nil`                                                |
| D01-06 | ConfigLoader   | Preflight: 模擬 GitHub 或 Agent 檢查失敗               | 轉發對應 Probe 的原始錯誤訊息                             |
| D01-07 | ConfigLoader   | Preflight: 模擬環境缺少 `git`                          | 回傳 `ErrGitNotInstalled`                                 |
| D01-08 | WorkspacePaths | Hash: 給定相同工作區絕對路徑                           | 產出一致的 8 碼 hex hash 字串                             |
| D01-09 | WorkspacePaths | Init: 首次執行與重複執行                               | 成功建立所有必要目錄，重複執行不報錯 (等冪性)             |
| D01-10 | WorkspacePaths | 路徑生成: `TaskYaml(id)`                               | 回傳格式為 `<ws>/.gogents/tasks/<id>/task.yaml`           |
| D01-11 | TaskStore      | List: 掃描任務目錄                                     | 回傳當前所有有效的 Task 列表，跳過格式錯誤的目錄          |
| D01-12 | TaskStore      | Get: 讀取存在的任務                                    | 回傳 Task 物件，且若 `issue_number` 為 0 則自動從 ID 修正 |
| D01-13 | TaskStore      | Put: 原子寫入測試                                      | 寫入過程後，`task.yaml` 內容正確且無暫存檔殘留            |
| D01-14 | TaskStore      | Transition: 合法狀態轉換 (如 `queued` -> `processing`) | 狀態成功變更，且 patch 欄位正確套用並存檔                 |
| D01-15 | TaskStore      | Transition: 非法狀態轉換 (如 `queued` -> `done`)       | 回傳 `ErrIllegalTransition` 且不更新 `task.yaml`          |
| D01-16 | TaskStore      | UpdateFields: 特定欄位更新                             | 成功更新如 `CommentID` 等欄位，且 `Status` 保持不變       |

## D02 Daemon-Sync (同步與維護模組)

| 編號   | 測試對象   | 測試情境 (Scenario)                                | 預期結果 (Expected)                                                             |
| :----- | :--------- | :------------------------------------------------- | :------------------------------------------------------------------------------ |
| D02-01 | Syncer     | Tick: 發現遠端新任務                               | 在本地 `TaskStore` 建立 status 為 `queued` 的新任務，並記錄 `sync.task_created` |
| D02-02 | Syncer     | Tick: 遠端任務已刪除                               | 本地任務狀態透過 `Transition` 變更為 `deleted`                                  |
| D02-03 | Syncer     | Tick: 遠端任務欄位更新但狀態不變                   | 透過 `UpdateFields` 更新本地任務的 source 欄位 (如 `UpdatedAt`)                 |
| D02-04 | Syncer     | Tick: 偵測到 Open 任務關閉且本地為 Processing      | 狀態變更為 `done` (符合 B3 規則)                                                |
| D02-05 | Syncer     | Tick: Failed 任務被手動加回 `gogents:queued` label | 若 `failure_count < max_failures` 則狀態重設為 `queued`                         |
| D02-06 | Syncer     | Tick: 源頭回報 transient 網路錯誤                  | 整輪 Sync 回報失敗，但不改動本地任務狀態                                        |
| D02-07 | Syncer     | applySourceTransition: 各種狀態轉換規則            | 純函數驗證：確保 reopen、closed 等邏輯符合設計文件 R03 表格                     |
| D02-08 | Daemon     | Startup: 啟動序列檢查                              | 依序呼叫 Preflight、Init、Repo 檢查，任一失敗即停止啟動                         |
| D02-09 | Daemon     | Startup: 重啟恢復 (Recovery)                       | 啟動時發現 `processing` 任務，自動轉回 `queued` 並清除 `started_at`             |
| D02-10 | Daemon     | Graceful Shutdown: 收到 SIGTERM                    | 設置 draining 旗標，完成當前 tick 後正常退出                                    |
| D02-11 | StateStore | Save/Load: `state.yaml` 持久化                     | 原子寫入當前 PID、Sync 時間與當前執行任務 ID                                    |
| D02-12 | Dispatcher | 選擇 TaskSource 實作                               | 依設定檔中的 `source.type` (如 `github_issue`) 傳回對應的實作物件               |

## D03 Task-Execution (任務執行模組)

| 編號   | 測試對象     | 測試情境 (Scenario)                                       | 預期結果 (Expected)                                                                                                    |
| :----- | :----------- | :-------------------------------------------------------- | :--------------------------------------------------------------------------------------------------------------------- |
| D03-01 | Runner       | Run: 正常執行流程 (Success)                               | 依序完成：狀態轉 Processing、抓取內容、寫入 `input.md`、準備 Git、呼叫 Agent、判定 Success、回報 GitHub、狀態轉 `done` |
| D03-02 | Runner       | Run: 任務輸入不變量防護                                   | 傳入 Status 非 `queued` 的任務時，直接報錯並終止，不改動狀態                                                           |
| D03-03 | Runner       | Run: 抓取內容時 Issue 已刪除                              | 狀態成功轉換為 `deleted` 並結束流程                                                                                    |
| D03-04 | Runner       | Run: Git Fetch 或 Checkout 失敗                           | 狀態轉為 `failed`，`failure_count` +1，並向 GitHub 回報失敗                                                            |
| D03-05 | Runner       | Run: Agent 執行完成但 `result.json` 缺失                  | 判定為 Case C (failed)，並記錄 `task.result_missing` 事件                                                              |
| D03-06 | Runner       | Run: `result.json` 內容 status 為 success 但缺少 `pr_url` | 判定為 Invalid (failed)，不接受該結果                                                                                  |
| D03-07 | Runner       | finalize: Coding 成功但回報 GitHub 失敗                   | 狀態停留在 `reporting`，並透過 `UpdateFields` 記錄 `reporting_error`                                                   |
| D03-08 | Runner       | finalize: Coding 失敗回報 GitHub 又失敗                   | 狀態保持為 `failed`，且不應因為回報失敗而反轉或重試                                                                    |
| D03-09 | Judge        | judge: 解析各種 `result.json`                             | 驗證 success/failed/missing/parse_error 各種 Verdict 判定正確                                                          |
| D03-10 | InputBuilder | renderInputMd: 模板渲染                                   | 驗證生成的 `input.md` 包含正確的 Base Prompt 與 Issue 內容/留言                                                        |
| D03-11 | Runner       | Context 取消處理                                          | 執行中收到取消信號，應立即停止並回傳 `ctx.Err()`，保持任務為 `processing`                                              |

## D04 Logger (日誌模組)

| 編號   | 測試對象 | 測試情境 (Scenario)       | 預期結果 (Expected)                                                                     |
| :----- | :------- | :------------------------ | :-------------------------------------------------------------------------------------- |
| D04-01 | Logger   | Log: 寫入 Info 等級事件   | 事件同時出現在 `daemon.jsonl` 與 stdout；格式為正確的 JSONL                             |
| D04-02 | Logger   | Log: 寫入 Error 等級事件  | 事件同時出現在 `daemon.jsonl`, `error.jsonl`, stdout, stderr                            |
| D04-03 | Logger   | Log: TS 時區驗證          | `ts` 欄位必須符合 Asia/Taipei (UTC+8) 格式，毫秒精度                                    |
| D04-04 | Logger   | Log: 欄位一致性           | 確保 `level`, `event`, `message` 為必填，且 `fields` 正確序列化為 nested object         |
| D04-05 | Logger   | Log: 互斥保護             | 多個 Goroutine 同時寫入時，日誌行不交錯，維持每行完整性                                 |
| D04-06 | Logger   | Open: 目錄權限問題        | 當 logs 目錄無法寫入時，`Open` 應回傳錯誤 (用於啟動序列攔截)                            |
| D04-07 | Logger   | B5: 寫檔失敗時的 fallback | 模擬檔案關閉或磁碟滿，Logger 應改為在 stderr 輸出 fallback 訊息，且不向呼叫端回傳 error |
| D04-08 | Logger   | Close: 關閉後的行為       | 呼叫 `Close()` 後再呼叫 `Log()` 應為 no-op，且不應產生 panic                            |

## D05 GitHub-Client (GitHub 串接模組)

| 編號   | 測試對象         | 測試情境 (Scenario)                      | 預期結果 (Expected)                                                                |
| :----- | :--------------- | :--------------------------------------- | :--------------------------------------------------------------------------------- |
| D05-01 | GitHubSource     | List: 成功取得 Issue 列表                | 正確解析 `gh issue list` 的 JSON 輸出，並轉換為 `SourceTask` 結構                  |
| D05-02 | GitHubSource     | Get: 查詢單一 Issue 詳情                 | 正確解析 `gh issue view` 的 JSON 輸出，並映射 `Title`, `UpdatedAt` 等欄位          |
| D05-03 | GitHubSource     | 錯誤分類 (Classify): 模擬 404 錯誤       | 當 stderr 包含 `not found` 時，回傳 `ErrSourceNotFound`                            |
| D05-04 | GitHubSource     | 錯誤分類 (Classify): 模擬 401 錯誤       | 當 stderr 包含 `authentication` 時，回傳 `ErrSourceAuth`                           |
| D05-05 | GitHubSource     | 錯誤分類 (Classify): 模擬網路超時        | 回傳 `ErrSourceTransient` 並允許重試                                               |
| D05-06 | GitHubSource     | 重試機制 (Retry): 指數退避驗證           | 模擬前兩次呼叫失敗後第三次成功，確保 `ghRunner` 會依序重試，且最後一次成功回傳結果 |
| D05-07 | GitHubSource     | 重試機制 (Retry): 達到上限失敗           | 三次 transient 錯誤後，回傳 `ErrSourceTransient` 並記錄 Error 等級日誌             |
| D05-08 | SourceProbe      | CheckSource: 檢查 `gh` 環境              | 驗證 `exec.LookPath("gh")` 與 `gh auth status` 的呼叫邏輯                          |
| D05-09 | FeedbackReporter | Report: 向 Issue 留言 (Stage 3)          | 驗證 `gh issue comment` 的參數組裝是否包含正確的 Body 與 Issue Number              |
| D05-10 | FeedbackReporter | Report: 切換 Issue Label (Stage 3)       | 驗證 `gh issue edit` 的 `--add-label` 與 `--remove-label` 組合邏輯                 |
| D05-11 | FeedbackReporter | Report: Label 不存在時自動建立 (Stage 3) | 檢測到 Label 缺失時，應先呼叫 `gh label create` 再加掛至 Issue                     |
| D05-12 | Mapping          | mapIssueToTask: 欄位映射功能             | 驗證 GitHub API 特有的 `OPEN`/`CLOSED` 字串能正確對應到系統內部 Enum               |

## D06 Agent-Runner (Agent 執行適配模組)

| 編號   | 測試對象      | 測試情境 (Scenario)                   | 預期結果 (Expected)                                                                         |
| :----- | :------------ | :------------------------------------ | :------------------------------------------------------------------------------------------ |
| D06-01 | AgentProbe    | CheckAgent: `gemini` 正常存在         | `exec.LookPath` 成功且 `gemini --version` 回傳 0，回傳 `nil`                                |
| D06-02 | AgentProbe    | CheckAgent: `gemini` 未安裝           | 回傳 `AgentProbeError` 且 Stage 為 `lookup`                                                 |
| D06-03 | AgentProbe    | CheckAgent: `gemini --version` 超時   | 10s 後回傳 `AgentProbeError` 且 Reason 為 `timeout`                                         |
| D06-04 | AgentExecutor | Run: 正常啟動並執行                   | 子行程 cwd 為指定工作目錄，stdin/stdout/stderr 正確對應檔案且 `task.agent_invoked` 事件觸發 |
| D06-05 | AgentExecutor | Run: 輸入檔不存在                     | 啟動失敗並記錄 `agent.spawn_failed`，回傳 `ErrAgentIO`                                      |
| D06-06 | AgentExecutor | Run: 子行程 ExitCode 非零             | `Run` 仍回傳 `nil` error，但 `RunResult` 中的 `ExitCode` 為真實值，且記錄 Warning 日誌      |
| D06-07 | AgentExecutor | Run: Context 取消 (Graceful Shutdown) | 被取消時子行程應結束，`RunResult` 標記 `Signaled=true` 並回傳 `ctx.Err()`                   |
| D06-08 | Dispatcher    | NewAgentProbeFromConfig               | 根據 `agent.type` (如 `gemini`, `claude`) 傳回對應的 Probe 實作                             |
| D06-09 | Dispatcher    | 未支援的 Agent 類型                   | 回傳 `ErrAgentUnsupportedType` 錯誤                                                         |
