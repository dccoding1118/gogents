# R11 - 本地任務源 (local)

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 `source.type: local` 的任務源:daemon 從本地 ticket inbox(`~/.am/tickets/`)接收 agent-manager (am) 產出的 ticket、依自身 label 認領、import 進 task store 執行、以 ticket 宣告的 `verify` 驗證、並將終態與結果回寫 ticket 檔。此為 R05(GitHub Issue 同步)+ R08(GitHub 回報)在本地 ticket 世界的並列實作,共用同一套 task store、狀態機(R03)與執行流程(R06、R07)。

## 定位:am 與 gogents 的分工

- **am(控制面)**:產出 ticket、治理 mission/DAG、HITL 審查。ticket 落 `~/.am/tickets/`,以 `worker`(label) 標記歸屬。am 不寫 worker 的 `.gogents/`。
- **gogents(執行面)**:讀 ticket inbox、依 label 與 `status == queued` 認領、執行、回寫終態。執行明細留 gogents 日誌,am 經 `gogents logs`(R12)取。
- **單向溝通**:`am → gogents`。am 不維護執行狀態副本;live 狀態向 `gogents runtime status`(R12)查,終態結果由 gogents 回寫 ticket 檔。

## ticket 檔格式

位置:`<source.path>/<ticket-id>.yaml`(預設 `~/.am/tickets/<id>.yaml`),YAML 格式,欄位由 am 定義。gogents **只讀執行所需欄位**,忽略其餘治理欄位(`mission` / `depends_on` / `approvals` / `risk` 等)。

| ticket 欄位     | gogents 用途                                              | 讀/寫 |
| --------------- | -------------------------------------------------------- | ----- |
| `id`            | 作為 `task_id`(原樣)                                   | 讀    |
| `worker`        | ticket 歸屬 label;與 `source.filter.label` 比對認領     | 讀    |
| `goal`          | input.md 的任務標題/摘要                                 | 讀    |
| `context`       | input.md 的任務內容                                      | 讀    |
| `verify`        | 驗證方式(`cmd`;見下)                                  | 讀    |
| `status`        | 認領閘門(僅 `queued` 可認領);終態回寫(`done` / `failed`) | 讀/寫 |
| `result`        | 終態結果回寫(`code` / `artifacts`;見下)              | 寫    |
| `updated_at`    | 比對變化偵測;回寫終態時更新                             | 讀/寫 |

> gogents 只讀取執行所需欄位、只回寫 `status` / `result` / `updated_at`;ticket 的其他治理欄位由 am 擁有,gogents 不觸碰。

## TaskSource(掃描與認領)

比照 `github_issue` 依 label 過濾:

- **掃描**:列出 `source.path` 下的 ticket 檔(`*.yaml`)
- **過濾**:先比對 `worker` 等於 `source.filter.label`,再要求 `status == queued`;兩者皆符才認領。`status` 是 am 的 dispatch 閘門——只有 `queued` 代表前置(`depends_on`)已滿足、可被 worker 認領;`blocked` / `done` / `failed` / `awaiting_review` / 空 / 無法解析一律不認領,交由 am 以狀態控制 dispatch 時機
- **import**:未在本地 task store 的 ticket → 建 `task.yaml`,`task_id` = ticket `id`,status=`queued`,`failure_count`=0,填入 source 欄位(ticket 路徑、label、`updated_at`)
- **執行中不重領**:gogents 於 `processing_started` 不回寫 ticket(只寫終態),被認領、執行中的 ticket 其 am `status` 仍為 `queued`,會持續出現在掃描結果;這由既有「以 task id 去重、已在 task store 者不重複匯入」機制吸收,不造成重複執行。終態(`done`/`failed`)回寫後 `status != queued`,自動不再被認領
- **多 daemon 共讀**:多個 worker daemon 共讀同一 inbox、各取各 label、無爭用(各 daemon 只認領自己 label 且 `queued` 的 ticket)

### 既有任務狀態變化與刪除偵測

- 比對 ticket 檔 `updated_at` 與本地 task.yaml:無變化不動作;有變化更新 source 欄位並依 R03 判斷狀態轉換
- **ticket 檔不存在**(被 am 刪除):對應 task 轉 `deleted`(終態),比照 R05 刪除偵測;不刪除本地 task 目錄
- reopen 等對應規則語義比照 R05,以 ticket 檔存在性與 `updated_at` 判定

## ContentFetcher(組裝 input.md)

由 ticket 檔內容組出 input.md(比照 github 由 issue 內容組 input,見 R06):

- Base Prompt(區塊 1)與 github 型別共用(固定英文指令、repo 子目錄指示)
- Task Content(區塊 2)由 ticket 的 `goal`(標題)與 `context`(內容)組裝,原樣帶入不翻譯
- ticket 檔讀取失敗(不存在/解析失敗)比照 R06 的內容查詢失敗處理

## SourceProbe(前置檢查)

啟動前置檢查(R02 階段 2):確認 `source.path`(預設 `~/.am/tickets`)存在且可讀。不存在或無讀權限即拒啟並記 error。

## verify 驗證

ticket 可宣告 `verify`,決定 coding 成敗(見 R07 路徑 A):

```yaml
verify:
  cmd: "pytest tests/…"        # exit code 0 = pass,非 0 = fail
  # 可選 expected artifacts 檢查
```

- daemon 於 agent 結束後,在 workspace 內執行 `verify.cmd`,以 exit code 判 pass/fail
- 執行期間 `runtime_state` 為 `verifying`(R04)
- ticket **未宣告 `verify`** 時 fallback 現行 coding verdict(讀 result.json 的 status/pr_url;見 R07 路徑 B)

## FeedbackReporter(終態回寫 ticket)

新增 `local` 型別 FeedbackReporter,於 `processing_started` / `success` / `failed` 觸發時,將終態與結果寫回 ticket 檔 `<source.path>/<id>.yaml`,對稱於 `github_issue` reporter 於 issue 留言 + 輪 label。am 讀自身 inbox 即得終態結果。

### 回寫內容

- 更新 `status`:`done`(coding 成功且回報成功)/ `failed`(coding 失敗)
- 更新 `updated_at`
- 寫入 `result`,依型別:
  - **code**:`{ type: code, pr_url, branch, commit }`
  - **artifacts**:`{ type: artifacts, items: [...] }`
- 回寫採原子寫入(寫臨時檔後 rename),避免與 am 讀取競態導致半寫

### 觸發時機對應

| 觸發                 | 回寫動作                                       |
| -------------------- | ---------------------------------------------- |
| `processing_started` | (可選)標記進入執行;不改 am 治理欄位          |
| `success`            | `status: done`、寫 `result`                    |
| `failed`             | `status: failed`、寫失敗 `result` / message    |

> gogents 只回寫執行段終態;ticket 的治理段狀態(`awaiting-review` / `approved` 等)由 am 依回寫結果推進,gogents 不觸碰。

## 與 github_issue 的關係

- `local` 與 `github_issue` 為並列的 source concrete,共用 task store、狀態機、執行流程、agent 呼叫
- 差異僅在 source adapter(掃描 ticket / 組 input / 回寫 ticket)與 verify 判定路徑;兩者互不影響,一次只有一種 `source.type` 生效

## 相關需求

- R02 - Runtime 設定檔(`source.type: local` 設定、path/label、前置檢查)
- R03 - 任務狀態模型(task_id 格式、source 欄位、狀態轉換共用)
- R04 - Daemon 生命週期(`verifying` / `reporting` runtime_state)
- R05 - GitHub Issue 同步(並列 concrete;掃描/刪除偵測語義比照)
- R06 - 任務執行流程(ContentFetcher 組 input.md、工作目錄)
- R07 - 結果處理(verify 判定路徑、result 型別)
- R08 - GitHub 狀態回報(並列 concrete;FeedbackReporter 契約比照)
- R12 - Runtime 註冊與觀測(am 經 runtime status / logs 查執行狀態)
