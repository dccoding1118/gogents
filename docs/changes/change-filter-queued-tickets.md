# CH-005 — local 任務源只認領 status=queued 的 ticket

## 1. 背景與動機

`agent-manager`（am）以 ticket 的 `status` 欄位作為 dispatch 閘門：ticket 生命週期為 `blocked → queued → …`，只有 `queued` 代表「前置已滿足、可被 worker 認領執行」。所有 ticket（含 `blocked`）皆存在單一 canonical 位置 `~/.am/tickets/<id>.yaml`（同時是 store 與 inbox）。

現況：gogents `local` 任務源的 `List`（`internal/source/local.go`）**只依 `filter.label` 過濾、不看 `status`**，會把同 label 的 **`blocked`** ticket 也認領執行，破壞 am 的相依（`depends_on`）排序。

本變更使 local source **只認領 `status == queued`** 的 ticket，讓 am 能以狀態控制 dispatch 時機。

## 2. 變更目標

1. `local` 任務源 `List` 在 label 過濾之上，追加 `status == queued` 過濾。
2. 非 `queued`（`blocked` / `done` / `failed` / `awaiting_review` / … / 空 / 無法解析）一律不認領。
3. `github_issue` 任務源與現行 coding 流程不受影響。

## 3. 具體需求規格

### 3.1 List 過濾

- `localTicketSource.List`：對每個 label 相符的 ticket，僅當 `status == "queued"` 時納入回傳；其餘略過。
- **狀態缺失/未知即不認領**：只有明確的 `queued` 被認領（am 一律寫入 `status`，正常 ticket 皆有值）。
- 過濾順序：先 label、後 status；兩者皆符才 `mapTicketToSourceTask`。

### 3.2 與既有去重的相容

- gogents local reporter 於 `processing_started` 不回寫 ticket（只寫終態 done/failed），故被認領、執行中的 ticket 其 am `status` 仍為 `queued`，會**持續**出現在 `List`。
- 這由 gogents 既有的「以 task id 去重、已在 task store 者不重複匯入」機制吸收：`List` 重覆回傳同一 `queued` ticket 不造成重複執行。
- 終態（done/failed）寫回後 `status != queued`，`List` 自動不再回傳，無重匯入。

### 3.3 邊界

- `Get` / `ContentFetcher` / `Verifier` / `Reporter` 不變（依 task id 直接讀寫指定 ticket）。本變更僅影響「新任務發現」的 `List`。

## 4. 交付物

- `internal/source/local.go`：`localTicketSource.List` 追加 `status == queued` 過濾（`ticketFile.Status` 已解析，改動小）。
- `internal/source/local_test.go`：新增案例——`blocked` 不被認領、`queued` 被認領、終態不被認領、狀態缺失不被認領、label 與 status 交叉過濾。

## 5. 相容性

- 僅影響 `source.type: local`；`github_issue` 不變。
- 對既有 am 流程為修正（原本會誤領 blocked）；無破壞性介面變更。

## 6. 關聯

- am reconciler 設計（dispatch = `blocked→queued` 轉換、單一 inbox）：`~/workspace/agent-manager/docs/designs/design-reconciler.md` §2。
- am ticket 狀態機：`~/workspace/agent-manager/docs/designs/design-schema.md` §5；`design.md` §4.2。
- 對映現行實作：`internal/source/local.go`（`List` line 122–153、`ticketFile.Status`）。
