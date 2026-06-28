## Why

`es-log ls` 對第一線探索的使用者（含 AI agent）來說雜訊過高。實測一個叢集列出 81 個 target，其中 44 個 alias 幾乎全是系統內部物件（`.kibana*`、`.fleet-*`、`.alerts-*`、`.security*`），datastream 也含 `.fleet-actions-results`、`.items-default`、`.lists-default` 等 dot-prefixed 系統項。這些 `.` 開頭的目標幾乎不會是查 log 的對象，卻佔據大半輸出，淹沒真正有意義的 datastream。Elasticsearch 的系統／隱藏物件慣例即以 `.` 前綴命名，因此「預設隱藏 `.` 開頭、需要時再用 flag 顯示」能大幅降低探索雜訊，貼合本 CLI「agent-friendly 探索入口」的定位。

## What Changes

- `es-log ls` 及其 `aliases` / `datastreams` 子命令預設**排除**名稱以 `.` 開頭的 target（alias 與 datastream 一致套用）。
- 新增布林 flag `--show-hidden`（預設 `false`），掛在 `ls` 命令為 persistent flag，三個入口一律繼承；帶上後輸出回到含 dot-prefixed 的完整清單（即現行行為）。
- 過濾在 cmd 層的 `runLs` 進行：於 `collectTargets` 取得 rows 後、render 之前依旗標剔除 `.` 開頭的 row，使 table / json / jsonl 三種輸出格式一致；`esclient` 維持純唯讀抓取不變。
- 過濾規則對三入口一致：明確下 `ls aliases` 時若該叢集 alias 全為系統物件，預設輸出可能為空清單並 exit 0（空清單本身即「此處只有系統 alias」的有效訊號）。

## Non-Goals (optional)

- **不**改動 `search`：search 以 `-t <target>` 明確指定目標，使用者點名 `.kibana` 等即為明示意圖，不套用任何隱藏過濾。
- **不**以 Elasticsearch 的 `index.hidden` setting 或 `expand_wildcards` 參數判定隱藏：改打額外 API 成本高，且抓不到「dot 命名但未標 hidden」者。隱藏與否一律以「名稱是否以 `.` 開頭」這個單一、可預期的客戶端規則判定。
- **不**為子命令（`aliases` / `datastreams`）提供與 `ls` 不同的隱藏語意（不做「明確下子命令就不過濾」的特例），維持單一規則好記。
- **不**新增除 `--show-hidden` 以外的過濾 flag（不加 glob / regex / prefix 自訂過濾）。

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `list-targets`: 新增「預設隱藏 `.` 開頭 target、`--show-hidden` 還原顯示」的 requirement，套用於 `ls` 及 `aliases` / `datastreams` 子命令。

## Impact

- Affected specs: `list-targets`
- Affected code:
  - New: (none)
  - Modified:
    - internal/cmd/ls.go — `newLsCommand` 註冊 persistent flag `--show-hidden` 並以區域變數綁定；`runLs` 簽章新增 `showHidden bool` 參數；於 `runLs` 內、`collectTargets` 之後依旗標過濾 `.` 開頭 target。`collectTargets` 與 `esclient` 簽章不動
    - internal/cmd/ls_test.go — 以獨立 fixture 新增預設隱藏與 `--show-hidden` 還原的行為測試；確認既有 `TestLsCombined` / `TestLsAliasesOnly` / `TestLsDatastreamsOnly` 等共用 dot-free fixture 的測試在預設過濾下仍通過、斷言不需改動
  - Removed: (none)
