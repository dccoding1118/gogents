# STATUS — gogents

## 未歸檔結論

尚未落 `docs/` 的決策；各項於對應變更單落檔後從此處刪除。

**專案節奏**

- v0.1.0 已公開釋出；gogents 本體視為功能完整，**暫停於此版本**。§2 各項均為延後、非進行中，逐一另開 session 處理。

**module path**

- v2 以後需加 `/v2` path 後綴。vanity import path 評估後不採用（需自架 meta 服務，過度設計）。

**指令**

- **不新增 `init` 指令**：`config.yaml` 已是 lazy 自動建立（`Load` 缺檔回空 registry + nil，`Upsert` 的 `atomicWrite` 會 `MkdirAll`），`runtime list` 不會 fail。專建 config.yaml 的 init 屬純儀式。環境前置檢查（git / gh / claude 可用性）若要做，另名 `doctor`（尚未實作，選配）。

**am 收斂**（am 側待辦，gogents 側已備妥）

- am 最終改為呼叫 `gogents runtime create`，屆時刪除 am 的 `internal/provision` materialize 與 `templates/`。gogents 新增指令不會弄壞 am，且 am 一律以絕對 `source.path` 寫入 `runtime.yaml`，走不到 CH-009 改的 inbox fallback。
- am 的 `package.sh` 改抓 gogents release 產物（CH-010 已備妥發佈管道），不再從 `~/workspace/gogents` 原始碼建置。

## 未完成任務

各項逐一開隔離 session 執行。

- [ ] **CLI 重構到 cobra**：核心分派仍是手動 `flag` + switch（`internal/cli` 的 `Dispatch`）；子命令已含 `start`、`logs`、`version`、`runtime {create,list,status,logs,remove}`。cobra 化為收斂手動分派的重構，慣例見 `go-house-conventions`。
- [ ] **（跨 repo，另排）am／mlp LICENSE 對齊**：am 補 `LICENSE`（現 README 明寫 all rights reserved，擋 fork）；mlp 補 `LICENSE` 檔＋README License 章。推廣文件標準時一併。
