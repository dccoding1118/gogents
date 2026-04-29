# R10 - 錯誤處理

> 主需求檔：[requirement.md](../requirement.md)

## 目的

本需求為橫切關注點,統一定義其他需求(R04、R05、R06、R07、R08)所提到的失敗分類、重試策略、退避機制、daemon panic 處理等共通規則,避免各需求各自定義造成不一致。

## 錯誤分類

依影響範圍與處理方式區分四類:

| 類別       | 範圍        | 處理方式                                   | 範例                             |
| ---------- | ----------- | ------------------------------------------ | -------------------------------- |
| 設定錯誤   | Daemon 層級 | 拒絕啟動或立即中止                         | runtime.yaml 欄位缺失、gh 未安裝 |
| 暫時性錯誤 | 操作層級    | 指數退避重試,耗盡後降級處理                | 網路閃斷、GitHub 5xx、API 限流   |
| 任務級錯誤 | 單一任務    | 標記 task 為 failed 或 reporting,繼續下個  | agent 崩潰、git push 失敗        |
| 致命錯誤   | Daemon 層級 | 寫入 error 後退出,依賴外部 supervisor 重啟 | 檔案系統不可寫、panic            |

## 設定錯誤

- 發生於 daemon 啟動階段(R02、R04)
- 寫入 `daemon.start_failed` 或 `daemon.precheck_failed` 事件(error)
- Daemon 以非零 exit code 退出
- 不重試,由使用者修正後重啟

## 暫時性錯誤重試策略

適用於以下情境:

- R05 的 gh 指令呼叫(sync 階段)
- R08 的 gh 指令呼叫(comment、label)
- R06 的 `gh issue view`、`git fetch`、`git clone`

### 退避參數

採用**帶抖動的指數退避**(exponential backoff with jitter):

| 參數         | 預設值                                  |
| ------------ | --------------------------------------- |
| 初始間隔     | 1 秒                                    |
| 最大間隔     | 30 秒                                   |
| 倍率         | 2                                       |
| 抖動         | 每次實際等待 = 計算值 × [0.5, 1.5] 隨機 |
| 最大重試次數 | 3 次                                    |

實際等待時間序列範例:約 1s、2s、4s(皆再加抖動)。

參數目前版本寫死在程式中,不放 runtime.yaml。若後續版本發現需要調整,再升級為可配置。

### 重試範圍界定

- 重試作用於**單一指令呼叫**,例如一次 `gh issue view`
- R08 的多個子步驟(comment、label)**各自**套用重試,不共用計數
- sync 週期內多個 issue 各自處理,某個 issue 耗盡重試不影響下個 issue

### 重試耗盡後的降級

- R05 sync:整個 sync 視為失敗,寫入 `sync.failed`,等待下次 `sync_interval` 再重試。不阻塞後續任務執行
- R05 單一 issue:略過該 issue,寫入 warning,本次 sync 其他 issue 照常處理
- R06 查詢 issue / git 操作:任務轉為 `failed`,`failure_count` +1
- R08 任一子步驟:R08 整體回傳 `failure`,由 R07 決定後續(coding 成功時轉 reporting、coding 失敗時維持 failed)

### 哪些錯誤不重試

- 明確的 not found(404):不重試,直接走對應邏輯(R05 轉 deleted)
- 明確的認證失敗(401):不重試,寫 error 後中止當前操作。使用者需重新 `gh auth login`
- 設定錯誤(例如 label 名稱非法):不重試

## 任務級錯誤

適用於單一任務執行失敗,不影響 daemon 或其他任務。

### 失敗分類與對應處理

| 失敗來源                          | 定義於 | 最終狀態  | failure_count |
| --------------------------------- | ------ | --------- | ------------- |
| Git 操作失敗                      | R06    | failed    | +1            |
| Agent CLI 啟動失敗                | R06    | failed    | +1            |
| Agent CLI 執行後 result.json 異常 | R07    | failed    | +1            |
| result.json status=failed         | R07    | failed    | +1            |
| Coding 成功但 R08 回傳失敗        | R07    | reporting | 不增加        |
| 對應 issue 被刪除                 | R05    | deleted   | 不增加        |

### failure_count 達上限

見 R03 與 R05:`failure_count >= max_failures` 後任務停駐於 `failed`,sync 不再自動轉回 queued。

## 致命錯誤

Daemon 無法繼續運作的情境:

- `.task-syncer/` 或 `~/.task-syncer/logs/` 目錄不可寫
- state.yaml 連續寫入失敗(超過 3 次)
- 程式 panic(nil pointer、unrecoverable goroutine panic 等)

### 處理方式

- 嘗試寫入 error.jsonl(若可以寫)與 stderr
- 若有進行中任務,盡量將 task.yaml status 保留為 processing,依賴下次啟動的重啟恢復(R04)
- 以非零 exit code 退出
- 不嘗試在程式內重啟自己,依賴外部 supervisor(systemd、tmux、或使用者手動)

### 外部 supervisor 建議

目前版本不綁定特定 supervisor 方案,但建議使用者在實際部署時用 systemd user service 或 tmux 包裝,以便 daemon 意外退出後自動重啟。詳細部署說明不在本需求範圍。

## 檔案寫入失敗處理

task.yaml、state.yaml 寫入失敗:

- 第 1~2 次失敗:寫入 `state.write_failed`(error),立即重試(不退避,因為通常是暫時的 I/O 問題)
- 連續 3 次失敗:視為致命錯誤,daemon 退出

日誌檔(daemon.jsonl / error.jsonl)寫入失敗:

- 寫入 stderr fallback 訊息
- 不重試、不中止 daemon
- 見 R09「寫入失敗處理」章節

## 事件層級對應

| 情境                         | level   | 事件                                            |
| ---------------------------- | ------- | ----------------------------------------------- |
| 暫時性錯誤重試中,最終成功    | warning | `github.cli_failed` 或對應 event                |
| 暫時性錯誤重試耗盡           | error   | `github.cli_failed`、`sync.failed` 等           |
| 任務級錯誤(任務轉 failed)    | warning | `task.failed`                                   |
| 任務級錯誤(任務轉 reporting) | warning | `task.reporting_failed`                         |
| 設定錯誤                     | error   | `daemon.start_failed`、`daemon.precheck_failed` |
| 致命錯誤                     | error   | 對應的具體事件(如 `state.write_failed`)         |

## 不做的部分

目前版本刻意不實作:

- **熔斷器(circuit breaker)**:不因連續失敗自動停止某類操作
- **失敗告警**:不整合 Slack / Email / Discord 等外部通知(未來由 admin 通道提供)
- **錯誤分析統計**:不做錯誤率統計與儀表板
- **自動復原動作**:daemon 不嘗試修復環境問題(例如自動 `gh auth login`)

## 與其他需求的關聯

R10 不新增額外的需求行為,而是統整各需求已提及的錯誤處理規則:

- R02 - 設定錯誤的啟動前置檢查
- R04 - 重啟恢復與關閉行為
- R05 - Sync 失敗與單一 issue 處理失敗
- R06 - Git / agent 失敗的任務級處理
- R07 - result.json 異常與 reporting 狀態分派
- R08 - gh 指令失敗的子步驟判定與回傳契約
- R09 - 日誌事件的層級對應

其他需求定義具體行為,R10 負責統一參數與分類,避免各自重複定義。若未來調整退避參數、錯誤分類或重試次數,只需修改本需求即可。
