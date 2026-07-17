# STATUS — gogents

## 未歸檔結論

公開化計畫（2026-07-16 定案）尚未落 `docs/` 的決策；各項於對應變更單落檔後從此處刪除。

**定位**（落點：CH-011）

- gogents 是**個人／本機 agent 的專業運作環境工具**，可獨立運作、不依賴 am。am 從「控制面／Tower」降格為 local source 的其中一種 ticket producer。

**module path**

- v2 以後需加 `/v2` path 後綴。vanity import path 評估後不採用（需自架 meta 服務，過度設計）。

**指令**

- **不新增 `init` 指令**：`config.yaml` 已是 lazy 自動建立（`Load` 缺檔回空 registry + nil，`Upsert` 的 `atomicWrite` 會 `MkdirAll`），`runtime list` 不會 fail。專建 config.yaml 的 init 屬純儀式。環境前置檢查（git / gh / claude 可用性）若要做，另名 `doctor`。
- release binary 無版本資訊（無 `version` 指令、未注入 ldflags），使用者拿到產物無從辨識版本。待 CH-011 或另案處理。

**am 收斂**

- am 最終改為呼叫 `gogents runtime create`，屆時刪除 am 的 `internal/provision` materialize 與 `templates/`。本階段**不動 am**：gogents 新增指令不會弄壞 am，且 am 一律以絕對 `source.path` 寫入 `runtime.yaml`，走不到 CH-009 改的 inbox fallback。
- am 的 `package.sh` 改抓 gogents release 產物（CH-010 已備妥發佈管道），不再從 `~/workspace/gogents` 原始碼建置。

**暫緩**

- 「把 CH-007～CH-010 的重複步驟標準化成全域規則或全域 skills」暫緩，待實際跑過一輪、確認哪些步驟真的重複後再抽，避免抽到臆測的流程。

## 未完成任務

各項逐一開隔離 session 執行。

- [ ] **CH-011 公開化定位重寫**：README／AGENTS／requirement.md／R01, R02, R04, R07, R11, R12／D01, D07, D08 拔除「gogents 是 am 的執行面」定位，改為 producer 中立敘述（am 續留為 producer 與 `--json` 消費者的實例）。程式碼註解同步：`internal/foundation/config.go`、`internal/foundation/home.go`、`internal/cli/query.go`、`internal/source/local_test.go`。
- [ ] **開源專案結構與文件標準檢視**：對照業界慣例盤點公開 repo 的必要內容（CONTRIBUTING／CODE_OF_CONDUCT／issue・PR template／PR CI workflow／SECURITY.md 等）與 `docs/` 結構，決定取捨後再考慮抽成 skill。
