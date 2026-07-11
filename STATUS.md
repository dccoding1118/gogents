# STATUS — gogents

> 熱副本，只放「接手者沒寫就會遺失」的在途交接 delta。已落 docs/程式/git 的結論不重複記在此。

## 在途工作

（目前無在途工作。）最近一次交付 CH-006（`GOGENTS_HOME` + `runtime remove`）已合併至 `main`（#5）。CH-002~CH-006 皆已上版合併。

## 專案座標

- 變更單：`docs/changes/change-*.md`（每個 CH-xxx 一份）。需求：`docs/requirements/`（R01–R12）。設計：`docs/designs/`（D01–D08）。測試規劃：`docs/tests/unit-tests.md`。
- 對接 agent-manager (am) 的跨系統契約需求在 `~/workspace/agent-manager/docs/`（PRD / design / deploy）；gogents 為 am 的執行面（單向 `am → gogents`）。
