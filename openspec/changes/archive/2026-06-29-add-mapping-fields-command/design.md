## Context

`es-log` 是 agent-first 的唯讀 Elasticsearch CLI，目前能列出 target（`ls`）與查詢/聚合（`search`），但沒有任何查詢欄位結構（mapping）的能力。要知道欄位與型別只能繞過 CLI 直接 curl `_mapping`。

上一個 change（improve-es-error-diagnosability）揭露 partial shard failure 的常見根因是**同一欄位在不同 backing index 的型別分歧**——一個 alias/datastream 會 fan-out 到多個 backing index，各自 mapping 可能不同（如某欄位一邊 `text`、一邊 `keyword`）。使用者目前只能在查詢失敗後被動發現，無法在查詢前主動檢視。

既有約束：
- 唯讀 client 有明文 endpoint 白名單（`readonly-es-client` spec），且有一條測試斷言「exported 方法集合剛好是 `{ListAliases, ListDataStreams, Search, GetMaxResultWindow, Ping}`」。新增方法必須同步更新白名單與該斷言。
- `output-format` spec 規定 search→jsonl、其餘命令→json。`fields` 沿用此規範，預設 json。
- 既有命令骨架：`ls` 的 `runLs → client.ListX → renderLs`、`search` 接 target 參數、`GetMaxResultWindow` 用 `escapeTarget` 組路徑打 `_settings`。新功能照抄這些既有模式。

## Goals / Non-Goals

**Goals:**

- 新增 `es-log fields <target>` 唯讀命令，攤平 `_mapping` 為「欄位路徑 → 型別」。
- 跨多個 backing index 合併同一欄位的型別，並在型別 ≥2 種時明確標示衝突。
- 三種輸出格式（json 預設、table、jsonl）皆呈現衝突；衝突欄位在 json/jsonl 帶 per-index breakdown。
- 維持唯讀保證：只新增一個 GET 端點，更新白名單。

**Non-Goals:**

- 不顯示 mapping 的其他 metadata（analyzer、format、ignore_above、dynamic 設定等）——只關注欄位路徑與型別。
- 「欄位僅存在於部分 index」不視為衝突、不另外標記（型別一致即查詢安全），以避免雜訊。
- 不提供欄位過濾 flag（如 `--field <pattern>`）或指定單一 backing index 的 flag——本次只做完整攤平視圖。
- 不改變 `search`/`ls` 既有行為，不改動 `DefaultFor` 既有對應（`fields` 自然落入 json 預設）。

## Decisions

### 新增 fields 命令沿用既有 ls 命令骨架

`es-log fields <target>` 接受剛好一個 target 參數（`cobra.ExactArgs(1)`），`RunE` 呼叫 `runFields`，流程對稱於 `runLs`：`loadConfig → formatFor("fields") → buildClient → client.GetMapping → renderFields`。target 字串原樣傳給 client（同 `search`，支援逗號分隔的多 target、alias、datastream、wildcard）。命令在 `NewRootCommand` 以 `newFieldsCommand(opts)` 註冊。

替代方案：做成 `ls fields <target>` 子命令。否決——`ls` 的語意是「列出 target 名稱」，欄位檢視是不同關注點，獨立頂層命令更清楚。

### GetMapping 在 client 層攤平並跨 index 彙整型別

唯讀 client 新增 `GetMapping(ctx, target)`，打 `GET /<target>/_mapping`（用既有 `escapeTarget`），回傳已攤平、已跨 index 合併的 `[]FieldType`，依欄位路徑排序。攤平與彙整放在 client 層（與 `ListAliases`/`ListDataStreams` 解析回應、`SearchResponse.ShardFailure()` 自帶判定的既有分工一致），命令層只負責渲染與衝突標示文字。

`_mapping` 回應形狀為 `{"<index>": {"mappings": {"properties": {...}}}}`。攤平規則：
- 葉節點（有 `type`、無 `properties`）→ 產生 `path → type`。
- 有 `properties` 的節點 → 遞迴進入子欄位並以 `path.` 為前綴；**不**為中間 object 節點本身產生列。
- 既無 `type` 也無 `properties` 的節點（dynamic/空 object）→ 產生 `path → object`。
- 葉節點帶 `fields`（multi-field）→ 對每個 multi-field 子名產生 `path.subname → subtype`。

跨 index 彙整：以 `map[path]map[index]type` 累積每個欄位在各 index 的型別；distinct 型別為去重後排序的集合，長度 >1 即衝突。

替代方案：client 只回傳原始 `_mapping` JSON、攤平交給命令層。否決——攤平與跨 index 彙整是有實質深度的純資料轉換，放 client 與既有解析分工一致，命令層保持輕薄。

### 型別衝突偵測與 per-index breakdown 的資料形狀

client 回傳型別：

```go
// FieldType is one flattened mapping field and the type(s) it resolves to across
// all backing indices of the target.
type FieldType struct {
    Name    string            // dotted path, e.g. "user.id"
    Types   []string          // distinct types across indices, sorted ascending
    ByIndex map[string]string // index name -> type; populated only when len(Types) > 1
}
```

`len(Types) == 1` 為型別一致；`> 1` 為衝突。`ByIndex` 只在衝突時填入，使非衝突輸出維持精簡、且大型 datastream（眾多 backing index）不會無謂膨脹。回傳 slice 依 `Name` 排序，三種格式輸出順序一致。

替代方案：所有欄位都帶 `ByIndex`。否決——datastream 常有大量 backing index，逐欄位逐 index 列出會讓輸出爆量，而非衝突欄位的 per-index 資訊是冗餘的。

### 三格式渲染：json 預設、table 標示衝突、jsonl per-line

`formatFor("fields")` 不在 `DefaultFor` 的 search 特例內，自然回傳 json（符合既有 output-format 規範），無需改動 `DefaultFor`。

- **json**（預設）：JSON 陣列，每個元素 `{"name": ..., "types": [...], "conflict": <bool>}`；`conflict` 為 true 時額外帶 `"indices": {"<index>": "<type>"}`。沿用 `output.WriteJSON`。
- **jsonl**：同上 row 形狀，每列一個 JSON 物件。沿用 `output.WriteJSONLines`。
- **table**：欄位 `FIELD / TYPE`，`TYPE` 為 `strings.Join(Types, ", ")`；衝突時於該列 `TYPE` 後綴 `  ⚠ conflict`。沿用 `output.RenderTable`。

命令層定義對應 json 形狀的 row 型別（類比 `lsRow`），把 `FieldType` 映射為輸出列。

## Implementation Contract

**Behavior（觀察得到的行為）：**

- `es-log -c <ctx> fields <target>` 對 target 的 `_mapping` 產生攤平的欄位清單；成功且無衝突時 exit 0。
- 巢狀欄位以 dotted path 呈現（`user.id`）；multi-field 以 `message.keyword` 呈現；無子欄位的 object 以型別 `object` 呈現。
- 同一欄位路徑在 target 解析到的多個 index 中出現 ≥2 種型別時，輸出標示為衝突：table 在該列後綴 `⚠ conflict` 並以逗號列出所有型別；json/jsonl 該列 `conflict: true` 並帶 `indices` per-index 型別對應。
- 欄位僅存在於部分 index（型別一致）不標示為衝突。
- 預設輸出格式為 json；`-o table`、`-o jsonl` 切換格式；非法 `-o` 值以 exit 2 拒絕（沿用既有 `formatFor` 路徑）。

**Interface / data shape：**

- 新 client 方法 `func (c *Client) GetMapping(ctx context.Context, target string) ([]esclient.FieldType, error)`，打 `GET /<target>/_mapping`。
- `esclient.FieldType{ Name string; Types []string; ByIndex map[string]string }`，`Types` 去重升冪排序，`ByIndex` 僅在 `len(Types) > 1` 時填入；回傳 slice 依 `Name` 升冪排序。
- 命令 `es-log fields <target>`（`cobra.ExactArgs(1)`），於 `NewRootCommand` 註冊。
- json/jsonl 列形狀：`{"name": string, "types": [string], "conflict": bool, "indices"?: {string: string}}`（`indices` 僅衝突時出現）。

**Failure modes：**

- target 不存在（ES 404 index_not_found）→ 沿用既有 `classifyESError`，friendly 訊息 + exit 4。
- 連線/認證失敗 → exit 3。非法參數/格式 → exit 2。
- 非 2xx 一律走既有 `classifyESError`/`Diagnostic()` 路徑，不新增錯誤處理分支。

**Acceptance criteria：**

- `go test ./...` 綠燈，涵蓋：攤平（巢狀/multi-field/object）、跨 index 型別一致、跨 index 型別衝突含 `ByIndex`、部分缺席不算衝突、三格式渲染（含 table 的 `⚠ conflict` 後綴與 json 的 `conflict`/`indices`）、排序穩定。
- `readonly-es-client` 白名單測試的 exported 方法集合斷言更新為包含 `GetMapping` 且仍無 generic request 方法；request-recording 測試確認只發出 whitelisted 的 GET 請求。
- `golangci-lint run` 0 issue；`gofmt` 乾淨。
- README exit code / 命令章節記載 `fields` 命令與衝突標示（僅用合成範例資料，不含任何真實 index/欄位內容）。

**Scope boundaries：**

- In scope：`fields` 命令、`GetMapping` client 方法、攤平/彙整/衝突偵測、三格式渲染、白名單更新、README 與測試。
- Out of scope：mapping 其他 metadata、欄位過濾 flag、指定單一 backing index、變更 `search`/`ls`/`DefaultFor` 既有行為。

## Risks / Trade-offs

- [大型 datastream 的 `_mapping` 回應可能很大] → 只攤平 properties 並彙整型別，輸出僅含欄位/型別（非整份 mapping）；`ByIndex` 只在衝突時填，避免輸出膨脹。
- [深層巢狀或不尋常 mapping 形狀（runtime fields、未知節點）導致攤平遺漏] → 攤平規則以「有無 `type`/`properties`/`fields`」為準，未知節點退化為 `object`；以單元測試覆蓋巢狀與 multi-field，runtime fields 列為已知不涵蓋範圍。
- [跨大量 backing index 彙整的成本] → 單一 GET 回應在記憶體內一次處理，無額外往返；複雜度與欄位數×index 數線性，對互動式查詢可接受。

## Open Questions

（無）
