# R04 - Daemon 生命週期

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 daemon 的啟動、主迴圈、關閉、重啟恢復等生命週期行為,以及 state.yaml 的格式與用途。

## 啟動指令

```text
gogents start --workspace <path>
```

- `--workspace` 為必填參數,指向 workspace 根目錄的絕對路徑
- Daemon 不從 `~/.gogents/config.yaml` 自動選擇 workspace,由呼叫者明確指定
- 一個 Daemon 行程只服務一個 workspace

## 啟動序列

Daemon 啟動時依序執行以下步驟,任一步驟失敗即中止啟動並以非零 exit code 離開。

1. **驗證 workspace 路徑**:確認路徑存在且為目錄
2. **讀取 runtime.yaml**:位置為 `<workspace>/.gogents/runtime.yaml`,找不到則報錯
3. **驗證 runtime.yaml**:依 R02 定義的驗證規則檢查所有欄位
4. **初始化日誌目錄**:建立 `~/.gogents/logs/<workspace-hash>/`,開啟 daemon.jsonl 與 error.jsonl 寫入控制代碼(見 R09)
5. **註冊 runtime**:將本 runtime 以 workspace 路徑為 key 寫入(upsert)`~/.gogents/config.yaml`(見 R12);與初始化日誌同屬啟動期的全域註冊動作。寫入失敗僅記 error、不中止啟動(config.yaml 為觀測輔助,非執行必要)
6. **檢查/建立 repo 目錄**:
   - 若 `<workspace>/<reponame>/` 不存在:執行 `git clone <repo.url> <reponame>`
   - 若已存在且為有效 git repo:執行 `git fetch origin`
   - 若已存在但非 git repo:中止啟動並報錯
7. **載入 state.yaml**:若不存在則建立空白預設內容,`runtime_state` 初始化為 `idle`
8. **重啟恢復**:掃描所有 task 目錄,將狀態為 `processing` 的任務重置為 `queued` 並寫入 warning 日誌
9. **進入主迴圈**:開始 sync 與 task 執行循環,`runtime_state` 依階段更新

## state.yaml 格式

位置:`<workspace>/.gogents/state.yaml`
職責:記錄 daemon 的當前執行狀態,供重啟恢復與未來 admin 通道查詢使用。

```yaml
daemon:
  pid: 12345
  started_at: "2026-04-07T10:00:00Z"
  workspace: "/Users/me/projects/my-workspace"

runtime_state: "idle"                # idle | executing | verifying | reporting | failed | broken

sync:
  last_sync_at: "2026-04-07T10:05:00Z"
  last_sync_status: "success"       # success | failed
  last_sync_error: null             # 若 failed 則記錄錯誤訊息

current_task:
  task_id: "github-42"              # 若無進行中任務則為 null
  started_at: "2026-04-07T10:06:00Z"
```

state.yaml 採用原子寫入(寫入臨時檔後 rename),避免 daemon 崩潰導致檔案損毀。

## runtime_state 顯式狀態

`runtime_state` 是 daemon 的顯式執行階段,供觀測 CLI(R12)與外部消費者直接讀取,免於由 `current_task` + `sync` 欄位間接推導。daemon 於各階段主動更新:

| 值           | 語義                                             | 進入時機                          |
| ------------ | ------------------------------------------------ | --------------------------------- |
| `idle`       | 無進行中任務,等待下一輪 sync                     | 啟動完成、任務收斂後              |
| `executing`  | 正在執行任務的 coding 階段(呼叫 agent CLI)      | 任務轉 processing、呼叫 agent 期間 |
| `verifying`  | 正在執行 ticket 宣告的 `verify` 驗證             | agent 結束後跑 verify 指令期間(見 R11) |
| `reporting`  | 正在回寫結果到任務來源                           | 執行 feedback 回報期間            |
| `failed`     | 最近一次任務以失敗收斂                           | 任務轉 failed 後,至下輪重新 executing 前 |
| `broken`     | daemon 偵測到自身無法繼續(啟動後不變量破壞等)   | 內部異常時                        |

- `runtime_state` 與 task 的 `status`(R03)不同層級:前者描述 **daemon 當下在做什麼**,後者描述 **單一任務的命運**
- **`down` 不落檔**:daemon 行程不存在時的狀態由查詢端(R12 `runtime status`)依 pid 不存在推得,不寫入 state.yaml(死掉的 daemon 無法自己寫)
- 更新採 state.yaml 原子寫入
- **命名**:gogents 的執行單位是 **runtime**(daemon＋workspace),故欄位為 `runtime_state`。消費者可經 `gogents runtime status` 查得後,於自身分層對應到其術語(例如 agent-manager 將同一實體稱為 **worker**)

## 主迴圈

Daemon 主迴圈採用簡單的時間觸發與序列化執行模型:

```text
loop:
    等待至下一個 sync_interval 觸發點
    執行任務來源同步(見 R05)
    掃描 tasks/ 目錄尋找 status=queued 的任務
    若存在 queued 任務:
        選擇其中一個(依 task_id 字典序,避免順序不確定)
        執行任務(見 R06)
        處理結果(見 R07)
        回報結果至任務來源(見 R08)
```

- 任務執行期間不觸發新的 sync,sync interval 從「上一次 sync 完成時間」開始計算
- 每個 sync 週期最多執行一個任務,其餘 queued 任務留到下一個 sync 週期
- 這是刻意的設計:依排程節奏執行,避免短時間連續呼叫 agent CLI 消耗 token,並讓未來「立即執行」需求由 admin 通道顯式觸發

## 關閉行為

Daemon 收到 `SIGTERM` 或 `SIGINT` 時:

- **無進行中任務**:立即寫入關閉日誌後退出
- **有進行中任務**:
  - 設定「關閉中」旗標,不再接受新任務
  - 等待當前任務的 agent CLI 程序結束(不強制 kill)
  - 完成當前任務的結果處理與 GitHub 回報
  - 寫入關閉日誌後退出
- 收到第二次信號:強制退出,當前任務狀態留在 `processing`,下次啟動時由重啟恢復流程處理

目前不實作優雅關閉的 timeout 機制,假設使用者會給足夠時間或接受強制退出。

## 重啟恢復

Daemon 啟動時掃描 `<workspace>/.gogents/tasks/*/task.yaml`:

- 狀態為 `processing` 的任務:代表前次 daemon 未正常結束,將狀態重置為 `queued`,清除 `execution.started_at`,寫入 warning 日誌說明「任務因前次 daemon 異常退出而重置」
- 重置時 `failure_count` **不增加**:這是 daemon 自身異常,不歸咎於任務本身
- 由於 R06 任務開始時會以 `git checkout -B task/<task-id>` 強制重置分支,前次 agent 已寫入但未 push 的 commit 會遺失;若已 push,新分支會強制覆蓋遠端(force push)。重啟即重跑任務,使用者需理解此行為
- 其他狀態的任務:維持原狀

重啟後的 sync 會正常同步 GitHub,將任何新變動反映到本地,並在主迴圈中重新處理被重置的任務。

## 相關需求

- R01 - Workspace 結構(路徑位置)
- R02 - Runtime 設定檔(啟動時讀取的設定來源)
- R03 - 任務狀態模型(重啟恢復涉及的狀態轉換、failure_count 在重啟時不增加)
- R05 - GitHub Issue 同步(主迴圈呼叫)
- R06 - 任務執行流程(主迴圈呼叫、分支重置行為)
- R09 - 日誌規範(啟動、關閉、重啟事件的日誌寫入)
- R11 - 本地任務源(`verifying` 階段對應 ticket verify)
- R12 - Runtime 註冊與觀測(啟動註冊、`runtime_state` / pid 查詢)
