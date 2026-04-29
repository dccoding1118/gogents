# R07 - 結果處理

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 agent CLI 結束後,daemon 如何讀取 result.json、判定 coding 階段成敗、呼叫 R08 進行 GitHub 回寫,並依回寫結果決定任務最終狀態(done / reporting / failed)。

## 觸發時機

由 R06 在 agent CLI 程序結束後立即呼叫,無論 agent exit code 為何都會進入本流程。

## result.json 格式

由 agent 在執行過程中依 input.md 的指示寫入 `<task-dir>/result.json`,格式:

```json
{
  "status": "success",
  "pr_url": "https://github.com/owner/repo/pull/123",
  "message": "Implemented email notification with unit tests. PR opened."
}
```

### 欄位定義

| 欄位       | 型別   | 必填           | 說明                                             |
|------------|--------|----------------|--------------------------------------------------|
| `status`   | string | 是             | `success` 或 `failed`                            |
| `pr_url`   | string | success 必填   | Agent 開立的 PR 網址                             |
| `message`  | string | 是             | 執行摘要(success)或失敗原因(failed)          |

## 處理流程

```text
1. 讀取 <task-dir>/result.json
2. 判定 coding 階段成敗
3. 若 coding 成功(達斷點):
   a. 先更新 task.yaml 的 execution.pr_url、finished_at
   b. 呼叫 R08 進行 GitHub 回寫
   c. R08 回傳成功 → status 轉為 done
   d. R08 回傳失敗 → status 轉為 reporting,記錄 reporting_error
4. 若 coding 失敗:
   a. status 轉為 failed,failure_count +1
   b. 呼叫 R08 進行 GitHub 回寫(失敗也要通知使用者)
   c. R08 成敗皆不再改變本地狀態(已是 failed 終點)
5. 寫入對應日誌事件
6. 清除 state.yaml 的 current_task
```

## Coding 階段判定

### Case A:result.json 存在、可解析、status=success 且 pr_url 存在

判定:**coding 階段成功**,進入 reporting 階段。

動作:

- 更新 task.yaml:`execution.finished_at` 為當前時間(Asia/Taipei)、`execution.pr_url` 為 result.json 的 pr_url
- status 改為 `reporting`,呼叫 R08
- R08 回傳成功:status 轉為 `done`,寫入 `task.completed` 事件(info,fields 含 pr_url、duration_ms)
- R08 回傳失敗:status 保持 `reporting`,`execution.reporting_error` 寫入 R08 回傳的失敗原因,寫入 `task.reporting_failed` 事件(warning,fields 含 pr_url、reporting_error)

### Case B:result.json 存在、可解析、status=failed

判定:**coding 階段失敗**。

動作:

- task.yaml 的 status 轉為 `failed`
- `execution.failure_count` +1
- `execution.finished_at` 設為當前時間
- `execution.pr_url` 保持 null
- 呼叫 R08 更新 GitHub(回報失敗)
- 寫入 `task.failed` 事件(warning,fields 含 message、failure_count、agent_exit_code)
- R08 本身的成敗不再改變 task status(已是 failed),R08 失敗時僅寫入 `github.cli_failed` 事件

### Case C:result.json 不存在、無法解析、或必填欄位缺失

原因可能:agent 崩潰、agent 未遵守 prompt 指示、檔案寫入失敗、status=success 但 pr_url 缺失等。

判定:**coding 階段失敗**。

動作:同 Case B,但 `task.failed` 事件的 message 明確標示類型:

- `result.json 不存在`
- `result.json 解析失敗: <錯誤訊息>`
- `result.json 缺少必填欄位: <欄位名>`
- `result.json status 值非預期: <值>`
- `result.json status=success 但 pr_url 缺失或格式異常`

### Case D:Agent 在 R06 階段就已失敗(git、查詢 issue 等)

R06 各步驟失敗時已將 task 轉為 `failed` 並 `failure_count` +1,本需求只需執行後續共通步驟(呼叫 R08 通知、清 current_task)。

## 檔案保留

目前版本**不做 archive、不刪除任何檔案**:

- input.md、output.jsonl、result.json 全部保留在 `<task-dir>/` 內
- 下次任務從 queued 轉為 processing 時(例如失敗後重跑),R06 會**覆寫** input.md、建議 truncate output.jsonl
- 歷次 result.json 會被新的覆寫,只保留最後一次

> 若未來有「保留歷次執行紀錄」需求,需在 task-dir 下加 `runs/<timestamp>/` 子目錄,屬後續版本範圍。

## 與 failure_count 的互動

- `failure_count` 僅在 Case B、Case C、以及 R06 的各失敗分支中 +1
- reporting 狀態 **不** 增加 failure_count(這不是 coding 失敗)
- R04 重啟恢復導致的 processing → queued 重置 **不** 增加 failure_count
- 達到 `max_failures` 後,R05 sync 階段不再將 failed 轉回 queued(見 R03、R05)

## R08 呼叫契約

本需求呼叫 R08 時,傳入的資訊:

- task_id
- 本次回報對應的階段結果(coding_success / coding_failed)
- pr_url(coding_success 時必填)
- message(result.json 的 message 或失敗原因描述)
- 是否達到 `max_failures`(影響 R08 comment 文字,僅 coding_failed 時有意義)

R08 回傳:

- `success` / `failure`
- 若 failure:錯誤原因字串(供寫入 `execution.reporting_error`)

詳細契約見 R08。

## 相關需求

- R03 - 任務狀態模型(processing → done/reporting/failed 轉換、failure_count、reporting 性質)
- R04 - Daemon 生命週期(state.yaml.current_task 清除)
- R06 - 任務執行流程(前一棒、Case D 的失敗來源)
- R08 - GitHub 狀態回報(後一棒,回傳成敗決定 done 或 reporting)
- R09 - 日誌規範(task.completed / task.failed / task.reporting_failed 事件)
- R10 - 錯誤處理
