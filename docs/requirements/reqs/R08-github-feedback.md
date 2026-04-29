# R08 - GitHub 狀態回報

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 daemon 將任務狀態變化回報到 GitHub Issue 的規則,包含進度 comment 維護、issue label 更新、負責人 @mention 時機,以及向呼叫方回傳成敗的契約。

## 觸發時機

本需求定義的操作由以下需求呼叫:

- **R05**:新任務建立時,初次建立進度 comment、加上初始 label
- **R07**:任務 coding 階段結束時(無論成功或失敗),更新 comment、切換 label、@mention

目前版本的狀態轉換中只有這兩個時機會呼叫 R08(queued → processing 雖然也是狀態變化,但不對此做 GitHub 回報,避免 comment 過度頻繁)。

## 存取方式

僅透過 `gh` CLI(與 R05 一致)進行 comment 建立、comment 更新、label 變更。具體使用的子指令、參數格式由實作階段依 `gh` 當下最新版本決定,本需求不寫死指令細節。

所有 gh 操作失敗都走 R10 的重試策略。

## 回傳契約

R08 作為被呼叫端,向 R05、R07 回傳明確的成敗結果:

```text
Input:
  - task_id
  - trigger: "task_created" | "coding_success" | "coding_failed"
  - pr_url: string (trigger=coding_success 時必填)
  - message: string (comment 要呈現的摘要或失敗原因)
  - failure_count: int (trigger=coding_failed 時使用)
  - max_failures: int (trigger=coding_failed 時使用,判斷是否已達上限)

Output:
  - result: "success" | "failure"
  - error: string (result=failure 時填寫,描述失敗子步驟)
```

### 成敗判定(選項 A)

R08 內部可能執行多個 gh 子步驟(comment、label 等)。判定規則:

- **只要有任一子步驟失敗**(例如 comment 成功但 label 失敗):整體回傳 `failure`,error 字段描述具體哪個子步驟失敗
- **全部子步驟成功**:回傳 `success`
- 這個嚴格判定是為了讓 reporting 狀態明確指向「有東西沒同步好」,避免模糊的部分成功

## 進度 Comment 策略

每個任務在對應 issue 下維護**單一**進度 comment,而非每次狀態變化都發新 comment。

### 初次建立(trigger=task_created)

- 由 R05 在新任務建立時呼叫
- 以 `gh` CLI 建立 comment
- 將回傳的 comment_id 存入 task.yaml 的 `feedback.comment_id`
- 寫入 `github.comment_created` 事件

### 後續更新(trigger=coding_success / coding_failed)

- 由 R07 在任務 coding 階段結束時呼叫
- 以 `gh` CLI 更新 task.yaml 的 `feedback.comment_id` 所對應的 comment
- 若 `comment_id` 為 null(例如初次建立時失敗):改為建立新 comment 並補寫 comment_id
- 寫入 `github.comment_updated` 事件

### Comment 內容模板

依 trigger 使用對應模板。時間戳一律使用 Asia/Taipei。

**task_created**:

```text
🤖 Task Syncer:任務已建立
- Task ID: github-42
- Runtime: my-project
- 狀態:queued
- 建立時間:2026-04-07 18:00:00 +08:00
```

**coding_success**:

```text
🤖 Task Syncer:任務完成 ✅
- Task ID: github-42
- 狀態:done
- PR: https://github.com/owner/repo/pull/123
- 完成時間:2026-04-07 18:30:00 +08:00
- 摘要:<message>

@windysky912 請審核結果。
```

**coding_failed(未達上限)**:

```text
🤖 Task Syncer:任務失敗 ❌
- Task ID: github-42
- 狀態:failed(第 1 / 3 次失敗)
- 失敗時間:2026-04-07 18:30:00 +08:00
- 原因:<message>

@windysky912 請審核結果。重開 issue 或重新加上觸發 label 可再次執行。
```

**coding_failed(達上限)**:

```text
🤖 Task Syncer:任務失敗 ❌(已達失敗上限)
- Task ID: github-42
- 狀態:failed(3 / 3 次,停止自動重試)
- 失敗時間:2026-04-07 18:30:00 +08:00
- 原因:<message>

@windysky912 需人工介入。可直接於本機重置 failure_count,或刪除 task 目錄讓系統重建。
```

## Label 管理

任務狀態變化時,daemon 負責同步 GitHub issue 的 label。

### Label 命名與命名空間

為避免與使用者自有 label 衝突,統一使用 `gogents:` 前綴:

- `gogents:queued`
- `gogents:processing`
- `gogents:done`
- `gogents:failed`
- `gogents:reporting`

觸發 label(runtime.yaml 的 `source.filter.label`)由使用者自訂,daemon 不觸碰這個 label。

### 切換邏輯

狀態變化時,一次性移除所有 `gogents:*` label 再加上新的狀態 label(具體指令由實作決定)。

- 寫入 `github.label_changed` 事件,fields 含 from / to
- 更新 task.yaml 的 `feedback.last_label`

trigger 對應的目標 label:

- `task_created` → `gogents:queued`
- `coding_success` → 由呼叫方決定(R07 若最終 done 則為 `gogents:done`,若 reporting 則 R08 自身已失敗,label 可能部分成功,走下述「Label 不存在時的建立」與錯誤回報)
- `coding_failed` → `gogents:failed`

### Label 不存在時的建立

`gogents:*` 系列 label 若在 repo 中尚未建立,label 變更會失敗。處理:

- 首次遇到 label 不存在錯誤時,以 `gh` CLI 自動建立(無色彩指定,用 gh 預設)
- 建立失敗:計為子步驟失敗,整體回傳 `failure`

## @mention 規則

- 僅在 trigger=coding_success 或 coding_failed 時 @mention 一次(透過 comment 更新內容達成)
- @mention 對象為 runtime.yaml 的 `assignee.github_username`
- 目前版本不支援多人 assignee
- 若 `assignee` 未設定:省略 @mention 部分,其餘 comment 內容照常

## 失敗處理細節

- 每個子步驟(comment、label、label 建立等)失敗時各自寫入 `github.cli_failed` 事件(level 依重試結果:全部失敗寫 error、中間失敗最終成功寫 warning)
- R10 的重試策略適用於每個子步驟內部的指數退避
- R10 重試結束仍失敗:該子步驟算失敗,R08 整體回傳 `failure`
- R08 **不改變本地 task 狀態**(狀態由 R05、R07 依 R08 回傳值決定)

## 相關需求

- R02 - Runtime 設定檔(assignee.github_username)
- R03 - 任務狀態模型(狀態轉換時機、reporting 狀態)
- R05 - GitHub Issue 同步(trigger=task_created 呼叫端)
- R07 - 結果處理(trigger=coding_success / coding_failed 呼叫端,依回傳值決定 done 或 reporting)
- R09 - 日誌規範(github.* 事件)
- R10 - 錯誤處理(gh 指令失敗的重試策略)
