# R05 - GitHub Issue 同步

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 daemon 從 GitHub Issue 同步任務到本地 tasks/ 目錄的邏輯,包含新任務偵測、既有任務狀態追蹤、來源刪除處理、以及 task.yaml 的建立與更新規則。

## 同步觸發時機

Sync 由 daemon 主迴圈觸發(見 R04):

- 依 runtime.yaml 的 `source.sync_interval` 定期觸發
- Sync 執行期間不接受重複觸發,採序列化執行
- Sync 與任務執行不並行

## GitHub 存取方式

Phase 1 **僅透過 `gh` CLI 存取 GitHub**,不直接呼叫 GitHub REST 或 GraphQL API,理由:

- 免處理 token 儲存與 OAuth 流程
- 使用者已完成 `gh auth login`,daemon 以使用者身份操作
- 所有 GitHub 互動(查詢、建立 comment、更新 label)統一經由 `gh` 指令
- 認證、retry 等行為由 `gh` 自身處理

Daemon 啟動前必須通過 R02 定義的「任務來源工具鏈檢查」(驗證 `gh` 安裝、授權狀態、網路連線、repo 權限),此處不再重複定義。

## 同步查詢範圍

每次 sync 執行兩類查詢,皆透過 `gh issue list` / `gh issue view` 子指令:

1. **帶觸發 label 的 open issue**:
   - 使用 `gh issue list --label <label> --state open --json ...`
   - 過濾條件依 runtime.yaml 的 `source.filter.label`
2. **本地已有 task 對應的 issue**:
   - 掃描 `<workspace>/.gogents/tasks/*/task.yaml`(略過 status=`deleted`)
   - 對每個任務以 `gh issue view <number> --json ...` 查詢最新狀態

`--json` 欄位至少需包含 `number, state, updatedAt, url`,body 與 comments 不在此階段查詢。

## 同步處理邏輯

### 新任務

條件: issue 帶觸發 label,且本地無對應 task 目錄。

動作:

- 建立 `<workspace>/.gogents/tasks/github-<issue-number>/` 目錄
- 寫入 task.yaml,status 為 `queued`,`failure_count` 為 0,填入 issue 的 url、state、updated_at
- issue body 與 comments **不存入 task.yaml**;執行階段(R06)再以 `gh issue view --json body,comments` 查詢完整內容寫入 input.md
- 呼叫 R08 在 issue 下建立進度 comment 與變更 label
- 寫入 sync 日誌 `sync.task_created`(見 R09)

### 既有任務狀態變化

條件:本地已有對應 task.yaml,且 status 不為 `deleted`。

比對 issue 的 `updatedAt` 與 task.yaml 的 `source.issue_updated_at`:

- **無變化**:不動作
- **有變化**:更新 task.yaml 的 `source.issue_state` 與 `source.issue_updated_at`,並依 R03 判斷是否改變 task status:
  - `processing` + issue `closed` → `done`(人工結案)
  - `done` + issue 重新 `open` 且 updatedAt 大於 task updated_at → `queued`
  - `failed` + issue 重新 `open` 或重新加觸發 label:
    - 若 `failure_count < max_failures`:轉為 `queued`
    - 若 `failure_count >= max_failures`:維持 `failed`,寫入 `sync.failure_lock_reached`(僅首次偵測)

### 來源 issue 被刪除

條件:本地有對應 task.yaml,但 `gh issue view` 回報 issue 不存在(stderr 含 not found 或 exit code 非 0 且訊息對應 404)。

動作:

- 將 task 狀態轉為 `deleted`(終態)
- 更新 task.yaml 的 `timestamps.updated_at`
- 不刪除 task 目錄,保留 input.md / output.jsonl / result.json
- 寫入 sync 日誌 `sync.task_deleted`
- 不嘗試在 GitHub 上更新 comment 或 label
- 後續 sync 不再查詢此任務

GitHub Issue 編號刪除後不可重複使用(未查證),deleted 終態不會與未來新任務衝突。

### 區別「issue 被刪除」與「gh 指令暫時失敗」

- **明確 not found(404)**:視為刪除,執行 deleted 轉換
- **其他錯誤(網路、5xx、auth、rate limit)**:視為暫時失敗,不改變 task 狀態,寫入 error 日誌,等待下次 sync 重試

## 同步結果日誌

每次 sync 結束必須寫入 `sync.completed` 事件至 daemon.jsonl,`fields` 包含:

- 查詢到的 issue 總數
- 新增、更新、標記為 deleted 的任務數量與 task_id 列表
- 無變動的任務數量
- sync 持續時間(毫秒)

即使「本次無新增或更新」也必須寫入。整體失敗時改寫入 `sync.failed`。

## 同步失敗處理

- 單一 issue 處理失敗: 記錄錯誤,繼續下一個,整體不中斷
- 整體查詢失敗: 中止本次 sync,寫入錯誤日誌,等待下次 interval 重試
- 連續多次失敗: 目前不做指數退避(R10 可能調整)

## task.yaml 欄位對應

| task.yaml 欄位             | gh issue view --json 欄位 |
|----------------------------|---------------------------|
| `task_id`                  | `github-<number>`         |
| `source.number`            | `number`                  |
| `source.url`               | `url`                     |
| `source.issue_state`       | `state`                   |
| `source.issue_updated_at`  | `updatedAt`               |
| `source.instance`          | 來自 runtime.yaml         |
| `source.type`              | 固定為 `github_issue`     |

## 相關需求

- R02 - Runtime 設定檔(啟動前置檢查、source 與 sync_interval 設定來源)
- R03 - 任務狀態模型(狀態轉換規則、deleted 終態、failure_count 互動)
- R04 - Daemon 生命週期(sync 觸發時機)
- R06 - 任務執行流程(執行時以 gh 查詢完整 issue 內容)
- R08 - GitHub 狀態回報(同樣透過 gh CLI 進行)
- R09 - 日誌規範(sync.* 事件定義)
- R10 - 錯誤處理(sync 失敗的重試策略)
