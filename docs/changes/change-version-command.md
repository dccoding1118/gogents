# CH-011 — 版本資訊（`gogents version` 與建置期注入）

## 1. 背景與動機

CH-010 建立了 release pipeline，推 `v*` tag 即產出多平台產物。但產物本身不帶版本資訊：binary 無 `version` 指令，建置亦未注入 tag 名。使用者下載後無從辨識手上的是哪一版，回報問題時無法指出版本，維護者亦無法要求重現。

版本資訊必須在第一顆公開產物送出前就位——binary 一旦流出，事後補的指令對已流出的副本無效。

## 2. 變更目標

1. 新增 `gogents version` 指令，輸出版本、commit、建置時間、Go 版本與目標平台。
2. release 產物於建置期經 `-ldflags -X` 注入 tag 名與 commit。
3. 未注入的建置（`go build` / `go install` / `mise run build`）仍輸出可辨識的資訊，不顯示空值。

## 3. 具體需求規格

### 3.1 版本來源優先序

`internal/version` 為唯一決策點。每個欄位依序取第一個有效值：

| 欄位 | 優先序 | 未注入時的來源 | 全無時 |
| --- | --- | --- | --- |
| `Version` | ldflags `-X` | `debug.ReadBuildInfo()` 的 `Main.Version`（`go install module@version` 帶得到） | `dev` |
| `Commit` | ldflags `-X` | build info 的 `vcs.revision`（`go build` 於 VCS 樹內帶得到）；`vcs.modified=true` 時後綴 `-dirty` | `unknown` |
| `Date` | ldflags `-X` | build info 的 `vcs.time` | `unknown` |
| `GoVersion` | — | `runtime.Version()` | — |
| `Platform` | — | `runtime.GOOS/GOARCH` | — |

`Main.Version` 於一般 `go build` 為 `(devel)`，視同無值，落到 `dev`。

ldflags 為首選而非單靠 build info：build info 的 `vcs.revision` 給的是 commit，給不出 tag 名，而 release 的身分是 tag。

### 3.2 指令介面

- `gogents version` 與 `gogents --version` 等價，皆走同一路由。
- 不提供 `--json`：版本是給人讀的識別資訊，`--json`（D08 §3.4）的既有用途是 am 消費 runtime 觀測資料，版本不在其內。下游需程式判讀時解析首行即可。
- 輸出格式（stdout，exit 0）：

```
gogents v0.1.0
commit:   1a2b3c4 (2026-07-17T08:30:00Z)
go:       go1.23.0
platform: darwin/arm64
```

commit 顯示前 7 碼；`unknown` 時原樣輸出，不縮短。

### 3.3 建置期注入

release pipeline 的 `go build` 於既有 `-ldflags="-s -w"` 追加：

| 符號 | 值 |
| --- | --- |
| `internal/version.version` | `${GITHUB_REF_NAME}`（tag 名） |
| `internal/version.commit` | `${GITHUB_SHA}` |
| `internal/version.date` | 建置時的 UTC RFC3339 時間戳 |

`mise run build` 不注入——本機建置的身分是工作樹狀態，由 build info 的 `vcs.revision` + `-dirty` 表達，比塞一個假 tag 名誠實。

## 4. 交付物

- `internal/version/version.go`：注入點與 `Info()` 解析（含 build info fallback）。
- `internal/version/version_test.go`：優先序與 fallback 的單元測試。
- `internal/cli/cli.go`：`Dispatch` 新增 `version` / `--version` 路由；usage 的 commands 列表補上 `version`。
- `internal/cli/version_test.go`：輸出格式與路由測試。
- `.github/workflows/release.yml`：`-ldflags` 追加三個 `-X`。
- `README.md`、`AGENTS.md`：指令清單補上 `version`。

## 5. 相容性

- 純新增：不觸碰既有子指令、設定契約或執行期路徑。
- 既有 `mise run build` 流程不變，僅輸出的 `version` 由 build info 推導。

## 6. 關聯

- release pipeline 與平台矩陣：CH-010。
- `--json` 的既有適用範圍：D08 §3.4。
